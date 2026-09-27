package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/learner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DeviceInfo struct {
	Name     string
	Platform string
}

type Identity struct {
	PrincipalID string
	Kind        string
	SessionID   string
	User        *User
}

type DeviceSession struct {
	ID         string
	DeviceName string
	Platform   string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

type TokenGrant struct {
	Identity              Identity
	Session               DeviceSession
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	GuestPreviewPromotion *GuestPreviewPromotion
}

type GuestPreviewPromotion struct {
	Status      string
	PreviewID   string
	RecordingID string
	Reason      string
}

func NormalizeDeviceInfo(info DeviceInfo) DeviceInfo {
	return DeviceInfo{
		Name:     truncateRunes(strings.TrimSpace(info.Name), 100),
		Platform: truncateRunes(strings.ToLower(strings.TrimSpace(info.Platform)), 40),
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func CreateAnonymousIdentity(ctx context.Context, database *db.DB, config TokenConfig, device DeviceInfo) (TokenGrant, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return TokenGrant{}, ErrIdentityUnavailable
	}
	now := config.now()
	principalID := uuid.NewString()
	expiresAt := now.Add(config.GuestTTL)
	tx, err := database.Begin(ctx)
	if err != nil {
		return TokenGrant{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO principals (id, kind, expires_at)
		VALUES ($1, 'guest', $2)`, principalID, expiresAt); err != nil {
		return TokenGrant{}, err
	}
	grant, err := createDeviceGrant(ctx, tx, config, Identity{PrincipalID: principalID, Kind: "guest"}, device, expiresAt)
	if err != nil {
		return TokenGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenGrant{}, err
	}
	return grant, nil
}

func RegisterIdentityUser(ctx context.Context, database *db.DB, config TokenConfig, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return TokenGrant{}, ErrIdentityUnavailable
	}
	passwordHash, err := HashPassword(credentials.Password)
	if err != nil {
		return TokenGrant{}, err
	}
	user := User{ID: uuid.NewString(), Email: credentials.Email, EnglishLevel: learner.DefaultEnglishLevel}
	tx, err := database.Begin(ctx)
	if err != nil {
		return TokenGrant{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`, user.ID, user.Email, passwordHash); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return TokenGrant{}, HTTPError{Message: "User with this email already exists.", Status: 409}
		}
		return TokenGrant{}, err
	}
	promotion, err := mergeGuestPrincipal(ctx, tx, guest, user.ID, config.now())
	if err != nil {
		return TokenGrant{}, err
	}
	grant, err := createDeviceGrant(ctx, tx, config, Identity{PrincipalID: user.ID, Kind: "user", User: &user}, device, config.now().Add(config.RefreshTTL))
	if err != nil {
		return TokenGrant{}, err
	}
	grant.GuestPreviewPromotion = promotion
	if err := tx.Commit(ctx); err != nil {
		return TokenGrant{}, err
	}
	return grant, nil
}

func LoginIdentityUser(ctx context.Context, database *db.DB, config TokenConfig, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return TokenGrant{}, ErrIdentityUnavailable
	}
	user, err := LoginUser(ctx, database, credentials.Email, credentials.Password)
	if err != nil {
		return TokenGrant{}, err
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return TokenGrant{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO principals (id, kind, user_id)
		VALUES ($1, 'user', $1)
		ON CONFLICT (id) DO NOTHING`, user.ID); err != nil {
		return TokenGrant{}, err
	}
	promotion, err := mergeGuestPrincipal(ctx, tx, guest, user.ID, config.now())
	if err != nil {
		return TokenGrant{}, err
	}
	grant, err := createDeviceGrant(ctx, tx, config, Identity{PrincipalID: user.ID, Kind: "user", User: &user}, device, config.now().Add(config.RefreshTTL))
	if err != nil {
		return TokenGrant{}, err
	}
	grant.GuestPreviewPromotion = promotion
	if err := tx.Commit(ctx); err != nil {
		return TokenGrant{}, err
	}
	return grant, nil
}

func createDeviceGrant(ctx context.Context, tx pgx.Tx, config TokenConfig, identity Identity, device DeviceInfo, sessionExpiresAt time.Time) (TokenGrant, error) {
	device = NormalizeDeviceInfo(device)
	sessionID := uuid.NewString()
	familyID := uuid.NewString()
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return TokenGrant{}, err
	}
	accessToken, accessExpiresAt, err := IssueAccessToken(config, identity.PrincipalID, sessionID, identity.Kind)
	if err != nil {
		return TokenGrant{}, err
	}
	now := config.now()
	if _, err := tx.Exec(ctx, `
		INSERT INTO device_sessions
		  (id, family_id, principal_id, device_name, platform, expires_at, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7)`,
		sessionID, familyID, identity.PrincipalID, device.Name, device.Platform, sessionExpiresAt, now); err != nil {
		return TokenGrant{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (id, session_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`, uuid.NewString(), sessionID, refreshHash, sessionExpiresAt, now); err != nil {
		return TokenGrant{}, err
	}
	session := DeviceSession{ID: sessionID, DeviceName: device.Name, Platform: device.Platform, ExpiresAt: sessionExpiresAt, LastSeenAt: now, CreatedAt: now}
	identity.SessionID = sessionID
	return TokenGrant{
		Identity: identity, Session: session,
		AccessToken: accessToken, AccessTokenExpiresAt: accessExpiresAt,
		RefreshToken: refreshToken, RefreshTokenExpiresAt: sessionExpiresAt,
	}, nil
}

func AuthenticateAccessToken(ctx context.Context, database *db.DB, config TokenConfig, token string) (*Identity, error) {
	claims, err := ParseAccessToken(config, token)
	if err != nil {
		return nil, err
	}
	now := config.now()
	var row struct {
		Kind             string
		UserID           *string
		PrincipalExpires *time.Time
		MergedInto       *string
		SessionExpires   time.Time
		SessionRevoked   *time.Time
		Email            string
		IsSubscriber     bool
		EnglishLevel     *string
	}
	err = database.QueryRow(ctx, `
		SELECT p.kind, p.user_id, p.expires_at, p.merged_into_principal_id,
		       s.expires_at, s.revoked_at,
		       COALESCE(u.email, ''),
		       COALESCE(u.is_subscriber AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > NOW()), FALSE),
		       u.english_level
		FROM device_sessions s
		JOIN principals p ON p.id = s.principal_id
		LEFT JOIN users u ON u.id = p.user_id
		WHERE s.id = $1 AND s.principal_id = $2
		LIMIT 1`, claims.SessionID, claims.PrincipalID).Scan(
		&row.Kind, &row.UserID, &row.PrincipalExpires, &row.MergedInto,
		&row.SessionExpires, &row.SessionRevoked, &row.Email, &row.IsSubscriber, &row.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidAccessToken
	}
	if err != nil {
		return nil, err
	}
	if row.Kind != claims.Kind || row.SessionRevoked != nil || !row.SessionExpires.After(now) || row.MergedInto != nil || (row.PrincipalExpires != nil && !row.PrincipalExpires.After(now)) {
		return nil, ErrInvalidAccessToken
	}
	identity := &Identity{PrincipalID: claims.PrincipalID, Kind: row.Kind, SessionID: claims.SessionID}
	if row.Kind == "user" {
		if row.UserID == nil {
			return nil, ErrInvalidAccessToken
		}
		level := learner.DefaultEnglishLevel
		if row.EnglishLevel != nil {
			level = learner.NormalizeEnglishLevel(*row.EnglishLevel)
		}
		identity.User = &User{ID: *row.UserID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}
	}
	return identity, nil
}
