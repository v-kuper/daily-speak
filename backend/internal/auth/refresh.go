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
)

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
