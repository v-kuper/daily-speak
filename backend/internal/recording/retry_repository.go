package recording

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type SQLRetryUnitOfWork struct{ db *db.DB }

func NewSQLRetryUnitOfWork(database *db.DB) *SQLRetryUnitOfWork {
	return &SQLRetryUnitOfWork{db: database}
}

func (unit *SQLRetryUnitOfWork) Execute(ctx context.Context, operation func(RetryTransaction) error) error {
	if unit == nil || unit.db == nil {
		return errors.New("recording database is not configured")
	}
	tx, err := unit.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := operation(&sqlRetryTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type sqlRetryTransaction struct{ tx pgx.Tx }

func (transaction *sqlRetryTransaction) Claim(ctx context.Context, userID string, recordingID string, jobID string) (bool, error) {
	result, err := transaction.tx.Exec(ctx, `
		UPDATE recordings
		SET status = 'processing',
		    processing_error = NULL,
		    processing_job_id = $3,
		    transcript = CASE WHEN processing_stage = 'transcribing' THEN '' ELSE transcript END,
		    suggestions = CASE WHEN processing_stage IN ('transcribing', 'suggestions') THEN '[]'::jsonb ELSE suggestions END,
		    strengths = CASE WHEN processing_stage IN ('transcribing', 'suggestions') THEN '[]'::jsonb ELSE strengths END,
		    strengths_status = CASE WHEN processing_stage IN ('transcribing', 'suggestions') THEN 'pending' ELSE strengths_status END,
		    strengths_job_id = CASE WHEN processing_stage IN ('transcribing', 'suggestions') THEN NULL ELSE strengths_job_id END,
		    analysis_checkpoints = CASE WHEN processing_stage = 'transcribing' THEN '{}'::jsonb ELSE analysis_checkpoints END,
		    corrected_transcript = '',
		    shadowing_status = 'pending',
		    shadowing_asset_id = NULL,
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW(),
		    shadowing_attempt_id = NULL
		WHERE id = $1 AND user_id = $2 AND status = 'failed'`, recordingID, userID, jobID)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() != 1 {
		return false, nil
	}
	if _, err := transaction.tx.Exec(ctx, `UPDATE processing_jobs SET state = 'cancelled', completed_at = NOW(), lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
        WHERE resource_id = $1 AND kind = 'recording.strengths' AND state IN ('queued', 'running', 'retry_wait')
        AND EXISTS (SELECT 1 FROM recordings WHERE id = $1 AND processing_stage IN ('transcribing', 'suggestions'))`, recordingID); err != nil {
		return false, err
	}
	if _, err := transaction.tx.Exec(ctx, `
		UPDATE interview_turns t
		SET corrected_answer_text = NULL, updated_at = NOW()
		FROM interview_sessions s
		WHERE t.session_id = s.id AND s.recording_id = $1`, recordingID); err != nil {
		return false, err
	}
	return true, nil
}

func (transaction *sqlRetryTransaction) Find(ctx context.Context, userID string, recordingID string) (Record, bool, error) {
	return findRecord(ctx, transaction.tx, userID, recordingID)
}

func (transaction *sqlRetryTransaction) Enqueue(ctx context.Context, jobID string, recordingID string) error {
	return workqueue.Enqueue(ctx, transaction.tx, workqueue.NewJob{
		ID: jobID, Kind: workqueue.KindRecordingProcess, ResourceID: recordingID,
		IdempotencyKey: "recording:" + jobID, MaxAttempts: 3,
	})
}

var _ RetryUnitOfWork = (*SQLRetryUnitOfWork)(nil)
