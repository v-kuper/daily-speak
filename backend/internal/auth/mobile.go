package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/domain"
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

func RegisterMobileUser(ctx context.Context, database *db.DB, config TokenConfig, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return TokenGrant{}, ErrIdentityUnavailable
	}
	passwordHash, err := HashPassword(credentials.Password)
	if err != nil {
		return TokenGrant{}, err
	}
	user := User{ID: uuid.NewString(), Email: credentials.Email, EnglishLevel: domain.DefaultEnglishLevel}
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
	if err := mergeGuestPrincipal(ctx, tx, guest, user.ID, config.now()); err != nil {
		return TokenGrant{}, err
	}
	grant, err := createDeviceGrant(ctx, tx, config, Identity{PrincipalID: user.ID, Kind: "user", User: &user}, device, config.now().Add(config.RefreshTTL))
	if err != nil {
		return TokenGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenGrant{}, err
	}
	return grant, nil
}

func LoginMobileUser(ctx context.Context, database *db.DB, config TokenConfig, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
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
	if err := mergeGuestPrincipal(ctx, tx, guest, user.ID, config.now()); err != nil {
		return TokenGrant{}, err
	}
	grant, err := createDeviceGrant(ctx, tx, config, Identity{PrincipalID: user.ID, Kind: "user", User: &user}, device, config.now().Add(config.RefreshTTL))
	if err != nil {
		return TokenGrant{}, err
	}
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

func mergeGuestPrincipal(ctx context.Context, tx pgx.Tx, guest *Identity, userPrincipalID string, now time.Time) error {
	if guest == nil {
		return nil
	}
	guestPrincipalID := strings.TrimSpace(guest.PrincipalID)
	guestSessionID := strings.TrimSpace(guest.SessionID)
	if guest.Kind != "guest" || guestPrincipalID == "" || guestSessionID == "" {
		return ErrInvalidGuest
	}
	var kind string
	var expiresAt time.Time
	var mergedInto *string
	err := tx.QueryRow(ctx, `
		SELECT p.kind, p.expires_at, p.merged_into_principal_id
		FROM principals p
		JOIN device_sessions s ON s.principal_id = p.id
		WHERE p.id = $1 AND s.id = $2
		  AND s.revoked_at IS NULL AND s.expires_at > $3
		FOR UPDATE OF p, s`, guestPrincipalID, guestSessionID, now).Scan(&kind, &expiresAt, &mergedInto)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidGuest
	}
	if err != nil {
		return err
	}
	if kind != "guest" || !expiresAt.After(now) {
		return ErrInvalidGuest
	}
	if mergedInto != nil {
		if *mergedInto == userPrincipalID {
			return nil
		}
		return ErrInvalidGuest
	}
	if _, err := tx.Exec(ctx, `
		UPDATE principals
		SET merged_into_principal_id = $2, merged_at = $3, updated_at = $3
		WHERE id = $1`, guestPrincipalID, userPrincipalID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO principal_merges (guest_principal_id, user_principal_id, merged_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (guest_principal_id) DO NOTHING`, guestPrincipalID, userPrincipalID, now); err != nil {
		return err
	}
	// Media ownership follows the principal in the same transaction as the
	// identity merge. Guest uploads are enabled by a later epic, but keeping the
	// transfer here prevents a future registration retry from orphaning an
	// already uploaded object or exposing it through the old principal.
	if _, err := tx.Exec(ctx, `
		UPDATE media_assets
		SET owner_principal_id = $2, updated_at = $3
		WHERE owner_principal_id = $1`, guestPrincipalID, userPrincipalID, now); err != nil {
		return err
	}
	return revokePrincipalSessionsTx(ctx, tx, guestPrincipalID, "principal_merged", now)
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
		level := domain.DefaultEnglishLevel
		if row.EnglishLevel != nil {
			level = domain.NormalizeEnglishLevel(*row.EnglishLevel)
		}
		identity.User = &User{ID: *row.UserID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}
	}
	return identity, nil
}

func RotateRefreshToken(ctx context.Context, database *db.DB, config TokenConfig, refreshToken string) (TokenGrant, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return TokenGrant{}, ErrIdentityUnavailable
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if !strings.HasPrefix(refreshToken, "dsr_v1_") {
		return TokenGrant{}, ErrInvalidRefreshToken
	}
	now := config.now()
	tx, err := database.Begin(ctx)
	if err != nil {
		return TokenGrant{}, err
	}
	defer tx.Rollback(ctx)
	var row struct {
		TokenID          string
		SessionID        string
		TokenExpires     time.Time
		ConsumedAt       *time.Time
		TokenRevoked     *time.Time
		PrincipalID      string
		Kind             string
		UserID           *string
		PrincipalExpires *time.Time
		MergedInto       *string
		SessionExpires   time.Time
		SessionRevoked   *time.Time
		DeviceName       string
		Platform         string
		SessionCreatedAt time.Time
		Email            string
		IsSubscriber     bool
		EnglishLevel     *string
	}
	err = tx.QueryRow(ctx, `
		SELECT rt.id, rt.session_id, rt.expires_at, rt.consumed_at, rt.revoked_at,
		       p.id, p.kind, p.user_id, p.expires_at, p.merged_into_principal_id,
		       s.expires_at, s.revoked_at, s.device_name, s.platform, s.created_at,
		       COALESCE(u.email, ''),
		       COALESCE(u.is_subscriber AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > NOW()), FALSE),
		       u.english_level
		FROM refresh_tokens rt
		JOIN device_sessions s ON s.id = rt.session_id
		JOIN principals p ON p.id = s.principal_id
		LEFT JOIN users u ON u.id = p.user_id
		WHERE rt.token_hash = $1
		FOR UPDATE OF rt, s`, hashRefreshToken(refreshToken)).Scan(
		&row.TokenID, &row.SessionID, &row.TokenExpires, &row.ConsumedAt, &row.TokenRevoked,
		&row.PrincipalID, &row.Kind, &row.UserID, &row.PrincipalExpires, &row.MergedInto,
		&row.SessionExpires, &row.SessionRevoked, &row.DeviceName, &row.Platform, &row.SessionCreatedAt,
		&row.Email, &row.IsSubscriber, &row.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return TokenGrant{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return TokenGrant{}, err
	}
	if row.ConsumedAt != nil || row.TokenRevoked != nil {
		if err := revokeSessionTx(ctx, tx, row.SessionID, "refresh_token_reuse", now); err != nil {
			return TokenGrant{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenGrant{}, err
		}
		return TokenGrant{}, ErrRefreshTokenReused
	}
	if row.SessionRevoked != nil || row.MergedInto != nil {
		return TokenGrant{}, ErrInvalidRefreshToken
	}
	if !row.TokenExpires.After(now) || !row.SessionExpires.After(now) || (row.PrincipalExpires != nil && !row.PrincipalExpires.After(now)) {
		if err := revokeSessionTx(ctx, tx, row.SessionID, "expired", now); err != nil {
			return TokenGrant{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TokenGrant{}, err
		}
		return TokenGrant{}, ErrRefreshTokenExpired
	}
	identity := Identity{PrincipalID: row.PrincipalID, Kind: row.Kind, SessionID: row.SessionID}
	if row.Kind == "user" {
		if row.UserID == nil {
			return TokenGrant{}, ErrInvalidRefreshToken
		}
		level := domain.DefaultEnglishLevel
		if row.EnglishLevel != nil {
			level = domain.NormalizeEnglishLevel(*row.EnglishLevel)
		}
		identity.User = &User{ID: *row.UserID, Email: row.Email, IsSubscriber: row.IsSubscriber, EnglishLevel: level}
	}
	newToken, newHash, err := newRefreshToken()
	if err != nil {
		return TokenGrant{}, err
	}
	newTokenID := uuid.NewString()
	accessToken, accessExpiresAt, err := IssueAccessToken(config, identity.PrincipalID, row.SessionID, identity.Kind)
	if err != nil {
		return TokenGrant{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (id, session_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`, newTokenID, row.SessionID, newHash, row.SessionExpires, now); err != nil {
		return TokenGrant{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET consumed_at = $2, replaced_by_token_id = $3
		WHERE id = $1`, row.TokenID, now, newTokenID); err != nil {
		return TokenGrant{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE device_sessions SET last_seen_at = $2, updated_at = $2 WHERE id = $1`, row.SessionID, now); err != nil {
		return TokenGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return TokenGrant{}, err
	}
	return TokenGrant{
		Identity:    identity,
		Session:     DeviceSession{ID: row.SessionID, DeviceName: row.DeviceName, Platform: row.Platform, ExpiresAt: row.SessionExpires, LastSeenAt: now, CreatedAt: row.SessionCreatedAt},
		AccessToken: accessToken, AccessTokenExpiresAt: accessExpiresAt,
		RefreshToken: newToken, RefreshTokenExpiresAt: row.SessionExpires,
	}, nil
}

func ListDeviceSessions(ctx context.Context, database *db.DB, principalID string) ([]DeviceSession, error) {
	rows, err := database.Query(ctx, `
		SELECT id, device_name, platform, expires_at, last_seen_at, created_at
		FROM device_sessions
		WHERE principal_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY created_at DESC`, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []DeviceSession{}
	for rows.Next() {
		var session DeviceSession
		if err := rows.Scan(&session.ID, &session.DeviceName, &session.Platform, &session.ExpiresAt, &session.LastSeenAt, &session.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func RevokeDeviceSession(ctx context.Context, database *db.DB, principalID string, sessionID string, reason string) (bool, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE device_sessions
		SET revoked_at = NOW(), revocation_reason = $3, updated_at = NOW()
		WHERE id = $1 AND principal_id = $2 AND revoked_at IS NULL`, sessionID, principalID, reason)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, NOW()) WHERE session_id = $1`, sessionID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func RevokeAllDeviceSessions(ctx context.Context, database *db.DB, principalID string, reason string) error {
	tx, err := database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := revokePrincipalSessionsTx(ctx, tx, principalID, reason, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func revokePrincipalSessionsTx(ctx context.Context, tx pgx.Tx, principalID string, reason string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE device_sessions
		SET revoked_at = COALESCE(revoked_at, $2), revocation_reason = COALESCE(revocation_reason, $3), updated_at = $2
		WHERE principal_id = $1`, principalID, now, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE refresh_tokens rt
		SET revoked_at = COALESCE(rt.revoked_at, $2)
		FROM device_sessions s
		WHERE rt.session_id = s.id AND s.principal_id = $1`, principalID, now)
	return err
}

func revokeSessionTx(ctx context.Context, tx pgx.Tx, sessionID string, reason string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE device_sessions
		SET revoked_at = COALESCE(revoked_at, $2), revocation_reason = COALESCE(revocation_reason, $3), updated_at = $2
		WHERE id = $1`, sessionID, now, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, $2) WHERE session_id = $1`, sessionID, now)
	return err
}
