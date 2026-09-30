package recording

import (
	"context"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SQLFeedbackReanalysisRepository struct{ db *db.DB }

func NewSQLFeedbackReanalysisRepository(database *db.DB) *SQLFeedbackReanalysisRepository {
	return &SQLFeedbackReanalysisRepository{database}
}
func (r *SQLFeedbackReanalysisRepository) ScheduleFeedback(ctx context.Context, owner, id, key string) (bool, error) {
	if r.db == nil {
		return false, ErrFeedbackUnavailable
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var status, transcript string
	err = tx.QueryRow(ctx, `SELECT status,transcript FROM recordings WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, owner).Scan(&status, &transcript)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recording_feedback_reanalysis_requests WHERE recording_id=$1 AND request_key=$2)`, id, key).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, tx.Commit(ctx)
	}
	if status != "ready" || transcript == "" {
		return false, ErrFeedbackConflict
	}
	// Keep attempts immutable. Retire only audio belonging to the original feedback.
	if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state='cancelled',completed_at=NOW(),lease_token=NULL,lease_owner=NULL,lease_expires_at=NULL,updated_at=NOW()
 WHERE state IN ('queued','running','retry_wait') AND (resource_id=$1 AND kind IN ('recording.process','recording.strengths','shadowing.synthesize')
 OR kind='recording.feedback_audio' AND resource_id IN(SELECT id FROM recording_feedback_audio WHERE recording_id=$1 AND attempt_id IS NULL))`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE media_assets SET attached_at=NULL,retention_until=NOW(),updated_at=NOW()
 WHERE id IN (SELECT audio_asset_id FROM recording_feedback_audio WHERE recording_id=$1 AND attempt_id IS NULL
 UNION SELECT shadowing_asset_id FROM recordings WHERE id=$1) AND state='ready'`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM recording_feedback_audio WHERE recording_id=$1 AND attempt_id IS NULL`, id); err != nil {
		return false, err
	}
	jobID := uuid.NewString()
	if _, err := tx.Exec(ctx, `UPDATE recordings SET analysis_pipeline='focused-v1',status='processing',processing_stage='suggestions',processing_job_id=$2,
 processing_error=NULL,focused_feedback=NULL,suggestions='[]',strengths='[]',strengths_status='pending',strengths_job_id=NULL,
 analysis_checkpoints='{}',corrected_transcript='',shadowing_status='pending',shadowing_asset_id=NULL,shadowing_error=NULL,shadowing_attempt_id=NULL,shadowing_updated_at=NOW() WHERE id=$1`, id, jobID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE interview_turns SET corrected_answer_text=NULL WHERE session_id IN(SELECT id FROM interview_sessions WHERE recording_id=$1)`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO recording_feedback_reanalysis_requests VALUES($1,$2,$3)`, id, key, jobID); err != nil {
		return false, err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: workqueue.KindRecordingProcess, ResourceID: id, IdempotencyKey: "recording:" + jobID, MaxAttempts: 3}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
