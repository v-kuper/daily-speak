package recording

import (
	"context"
	"encoding/json"
	"errors"

	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

func (r *SQLProcessingRepository) ExecuteStrengths(ctx context.Context, operation func(StrengthsTransaction) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := operation(&sqlStrengthsTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type sqlStrengthsTransaction struct{ tx pgx.Tx }

func (t *sqlStrengthsTransaction) LockOwned(ctx context.Context, owner, recordingID string) (StrengthsRetrySource, bool, error) {
	var source StrengthsRetrySource
	err := t.tx.QueryRow(ctx, `SELECT status, COALESCE(processing_stage, ''), strengths_status, transcript
        FROM recordings WHERE id = $1 AND user_id = $2 FOR UPDATE`, recordingID, owner).
		Scan(&source.Status, &source.Stage, &source.StrengthsStatus, &source.Transcript)
	if errors.Is(err, pgx.ErrNoRows) {
		return StrengthsRetrySource{}, false, nil
	}
	return source, err == nil, err
}

func (t *sqlStrengthsTransaction) Enqueue(ctx context.Context, recordingID, jobID string) error {
	if _, err := t.tx.Exec(ctx, `UPDATE recordings SET strengths_status = 'processing', strengths_job_id = $2 WHERE id = $1`, recordingID, jobID); err != nil {
		return err
	}
	return enqueueStrengths(ctx, t.tx, recordingID, jobID)
}

func enqueueStrengths(ctx context.Context, tx pgx.Tx, recordingID, jobID string) error {
	return workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: workqueue.KindRecordingStrengths,
		ResourceID: recordingID, IdempotencyKey: "strengths:" + jobID, MaxAttempts: 3})
}

func (r *SQLProcessingRepository) LoadStrengthsWork(ctx context.Context, job ProcessingJob) (ProcessingWork, bool, error) {
	var work ProcessingWork
	var suggestions []byte
	err := r.db.QueryRow(ctx, `SELECT r.transcript, r.suggestions, r.topic, r.practice_type, r.photo_object,
		COALESCE((SELECT english_level FROM interview_sessions WHERE recording_id = r.id), u.english_level),
		(SELECT id FROM interview_sessions WHERE recording_id = r.id)
		FROM recordings r JOIN users u ON u.id = r.user_id
		WHERE r.id = $1 AND r.strengths_status = 'processing' AND r.strengths_job_id = $2`, job.ResourceID, job.ID).
		Scan(&work.Transcript, &suggestions, &work.Topic, &work.PracticeType, &work.PhotoObject, &work.EnglishLevel, &work.InterviewSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcessingWork{}, false, nil
	}
	work.Suggestions = NormalizeSuggestions(suggestions, 0)
	return work, err == nil, err
}

func (r *SQLProcessingRepository) SaveStrengths(ctx context.Context, job ProcessingJob, strengths []Strength) error {
	payload, err := json.Marshal(withoutStrengthReferences(strengths))
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireRecordingLease(ctx, tx, job); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE recordings SET strengths = $3::jsonb, strengths_status = 'ready'
		WHERE id = $1 AND strengths_job_id = $2 AND strengths_status = 'processing'
		AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $2 AND state = 'running' AND lease_token = $4 AND lease_expires_at > NOW())`,
		job.ResourceID, job.ID, string(payload), job.LeaseToken)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workqueue.ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (r *SQLProcessingRepository) FinalizeStrengthsFailure(ctx context.Context, tx pgx.Tx, jobID, recordingID string) error {
	_, err := tx.Exec(ctx, `UPDATE recordings SET strengths_status = 'failed'
		WHERE id = $1 AND strengths_job_id = $2 AND strengths_status = 'processing'`, recordingID, jobID)
	return err
}
