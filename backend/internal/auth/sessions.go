package auth

import (
	"context"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

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
