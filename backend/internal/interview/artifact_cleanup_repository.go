package interview

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func retireSessionArtifacts(ctx context.Context, tx pgx.Tx, sessionID string) error {
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state='cancelled',completed_at=NOW(),lease_token=NULL,lease_owner=NULL,lease_expires_at=NULL,updated_at=NOW()
 WHERE kind='interview.process' AND state IN ('queued','running','retry_wait')
 AND (resource_id=$1 OR payload->>'sessionId'=$1 OR resource_id IN(SELECT id FROM interview_turns WHERE session_id=$1))`, sessionID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE media_assets SET attached_at=NULL,retention_until=NOW(),updated_at=NOW()
 WHERE state NOT IN ('deleting','deleted') AND id IN (
 SELECT audio_asset_id FROM interview_question_artifacts WHERE session_id=$1
 UNION SELECT manifest_asset_id FROM interview_question_artifacts WHERE session_id=$1
 UNION SELECT audio_asset_id FROM interview_turns WHERE session_id=$1
 UNION SELECT asset_id FROM media_uploads WHERE interview_session_id=$1)`, sessionID)
	return err
}

func (r *SQLRepository) expireArtifactSessions(ctx context.Context) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM interview_sessions WHERE expires_at<=NOW()
 AND recording_id IS NULL AND guest_preview_id IS NULL AND status IN ('preparing','ready','recording','failed','cancelled')
 ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT 100`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := retireSessionArtifacts(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM interview_sessions WHERE id=$1`, id); err != nil {
			return err
		}
	}
	// Linked sessions are retained for replay and saved-answer attempts.
	if _, err := tx.Exec(ctx, `WITH expired AS (SELECT id FROM interview_sessions WHERE status='finalizing' AND expires_at<=NOW()
 AND (recording_id IS NOT NULL OR guest_preview_id IS NOT NULL) ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 100)
 UPDATE interview_sessions s SET status='finalized',updated_at=NOW() FROM expired e WHERE s.id=e.id`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
