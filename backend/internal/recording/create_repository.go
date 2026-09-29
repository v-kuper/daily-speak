package recording

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type SQLCreateUnitOfWork struct{ db *db.DB }

func NewSQLCreateUnitOfWork(database *db.DB) *SQLCreateUnitOfWork {
	return &SQLCreateUnitOfWork{db: database}
}

func (u *SQLCreateUnitOfWork) Execute(ctx context.Context, operation func(CreateTransaction) error) error {
	if u == nil || u.db == nil {
		return errors.New("recording database is not configured")
	}
	tx, err := u.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := operation(&sqlCreateTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type sqlCreateTransaction struct{ tx pgx.Tx }

func (t *sqlCreateTransaction) LockQuota(ctx context.Context, userID string, now time.Time) (quota.RecordingQuota, error) {
	return quota.LockRecordingQuota(ctx, t.tx, userID, now)
}

func (t *sqlCreateTransaction) Find(ctx context.Context, userID string, recordingID string) (Created, bool, error) {
	var created Created
	err := t.tx.QueryRow(ctx, `
		SELECT id, topic, duration, timestamp, status, transcript,
		       corrected_transcript, suggestions, processing_stage, practice_type,
		       photo_object, processing_error, shadowing_status, shadowing_error,
		       shadowing_updated_at, audio_asset_id, photo_asset_id,
		       (SELECT s.id FROM interview_sessions s WHERE s.recording_id = recordings.id)
		FROM recordings
		WHERE id = $1 AND user_id = $2`, recordingID, userID).Scan(createdDestinations(&created)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Created{}, false, nil
	}
	return created, err == nil, err
}

func (t *sqlCreateTransaction) LockMedia(ctx context.Context, principalID string, assetID string, purpose string) error {
	var found string
	err := t.tx.QueryRow(ctx, `
		SELECT id
		FROM media_assets
		WHERE id = $1 AND owner_principal_id = $2 AND purpose = $3
		  AND state = 'ready' AND attached_at IS NULL AND deleted_at IS NULL
		FOR UPDATE`, assetID, principalID, purpose).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCreateMediaNotFound
	}
	return err
}

func (t *sqlCreateTransaction) Insert(ctx context.Context, command CreateCommand) (Created, error) {
	var created Created
	err := t.tx.QueryRow(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript,
		   suggestions, practice_type, photo_object,
		   status, processing_stage, processing_job_id, audio_asset_id, photo_asset_id)
		VALUES
		  ($1, $2, $3, $4, $5, '', '', '[]'::jsonb, $6, $10,
		   'processing', 'transcribing', $7, $8, $9)
		RETURNING id, topic, duration, timestamp, status, transcript,
		          corrected_transcript, suggestions, processing_stage, practice_type,
		          photo_object, processing_error, shadowing_status, shadowing_error,
		          shadowing_updated_at, audio_asset_id, photo_asset_id, NULL::text`,
		command.RecordingID, command.UserID, command.Input.Topic, command.Input.Duration,
		command.Input.Timestamp, command.Input.PracticeType, command.JobID,
		command.Input.AudioAssetID, command.Input.PhotoAssetID, command.Input.PhotoObject,
	).Scan(createdDestinations(&created)...)
	return created, err
}

func (t *sqlCreateTransaction) LinkInterview(ctx context.Context, sessionID, principalID, userID, recordingID string) error {
	result, err := t.tx.Exec(ctx, `
		UPDATE interview_sessions
		SET recording_id = $4, status = 'finalizing', updated_at = NOW()
		WHERE id = $1 AND owner_principal_id = $2 AND user_id = $3
		  AND status = 'recording' AND recording_id IS NULL AND guest_preview_id IS NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM interview_turns t
		    WHERE t.session_id = interview_sessions.id AND NOT t.skipped AND t.transcript_status <> 'ready'
		  )
		  AND EXISTS (
		    SELECT 1 FROM interview_turns t
		    WHERE t.session_id = interview_sessions.id AND NOT t.skipped AND t.transcript_status = 'ready'
		  )`,
		sessionID, principalID, userID, recordingID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrInterviewSessionUnavailable
	}
	return nil
}

func (t *sqlCreateTransaction) AttachMedia(ctx context.Context, principalID string, assetIDs []string) error {
	result, err := t.tx.Exec(ctx, `
		UPDATE media_assets
		SET attached_at = NOW(), retention_until = NULL, updated_at = NOW()
		WHERE id = ANY($1::text[]) AND owner_principal_id = $2
		  AND state = 'ready' AND attached_at IS NULL`, assetIDs, principalID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != int64(len(assetIDs)) {
		return ErrCreateMediaNotFound
	}
	return nil
}

func (t *sqlCreateTransaction) EnqueueProcessing(ctx context.Context, command CreateCommand) error {
	return workqueue.Enqueue(ctx, t.tx, workqueue.NewJob{
		ID: command.JobID, Kind: workqueue.KindRecordingProcess,
		ResourceID: command.RecordingID, IdempotencyKey: "recording.create:" + command.RequestDigest,
		MaxAttempts: 3,
	})
}

func createdDestinations(created *Created) []any {
	return []any{
		&created.ID, &created.Topic, &created.Duration, &created.Timestamp,
		&created.Status, &created.Transcript, &created.CorrectedTranscript,
		&created.SuggestionsJSON, &created.ProcessingStage, &created.PracticeType,
		&created.PhotoObject, &created.ProcessingError, &created.ShadowingStatus,
		&created.ShadowingError,
		&created.ShadowingUpdatedAt, &created.AudioAssetID, &created.PhotoAssetID,
		&created.InterviewSessionID,
	}
}
