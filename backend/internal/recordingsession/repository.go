package recordingsession

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type SQLRepository struct{ db *db.DB }

func NewSQLRepository(database *db.DB) *SQLRepository {
	return &SQLRepository{db: database}
}

func (repository *SQLRepository) Create(ctx context.Context, command StartCommand) error {
	if err := repository.configured(); err != nil {
		return err
	}
	_, err := repository.db.Exec(ctx, `
		INSERT INTO recording_upload_sessions
		  (id, user_id, topic, duration, timestamp, practice_type, photo_data_url, photo_object)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		command.ID, command.UserID, command.Topic, command.Duration, command.Timestamp,
		command.PracticeType, command.PhotoDataURL, command.PhotoObject,
	)
	return err
}

func (repository *SQLRepository) Load(ctx context.Context, userID string, sessionID string) (Session, bool, error) {
	if err := repository.configured(); err != nil {
		return Session{}, false, err
	}
	var session Session
	err := repository.db.QueryRow(ctx, `
		SELECT id, user_id, topic, duration, timestamp, practice_type,
		       photo_data_url, photo_object, audio_extension, chunk_count,
		       status, recording_id
		FROM recording_upload_sessions
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, sessionID, userID).Scan(sessionDestinations(&session)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	return session, err == nil, err
}

func (repository *SQLRepository) UpdateChunk(ctx context.Context, sessionID string, userID string, extension string, chunkCount int) error {
	if err := repository.configured(); err != nil {
		return err
	}
	result, err := repository.db.Exec(ctx, `
		UPDATE recording_upload_sessions
		SET audio_extension = COALESCE(audio_extension, $3),
		    chunk_count = GREATEST(chunk_count, $4),
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status = 'open'`,
		sessionID, userID, extension, chunkCount)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrFinalized
	}
	return nil
}

func (repository *SQLRepository) UpdateFinal(ctx context.Context, sessionID string, userID string, extension string) error {
	if err := repository.configured(); err != nil {
		return err
	}
	result, err := repository.db.Exec(ctx, `
		UPDATE recording_upload_sessions
		SET audio_extension = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status = 'open'`, sessionID, userID, extension)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrFinalized
	}
	return nil
}

func (repository *SQLRepository) GetQuota(ctx context.Context, userID string, subscriber bool) (quota.RecordingQuota, error) {
	if err := repository.configured(); err != nil {
		return quota.RecordingQuota{}, err
	}
	return quota.GetRecordingQuota(ctx, repository.db, userID, &subscriber)
}

func (repository *SQLRepository) LoadRecording(ctx context.Context, userID string, recordingID string) (Recording, bool, error) {
	if err := repository.configured(); err != nil {
		return Recording{}, false, err
	}
	var recording Recording
	err := repository.db.QueryRow(ctx, recordingSelect+`
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, recordingID, userID).Scan(recordingDestinations(&recording)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recording{}, false, nil
	}
	return recording, err == nil, err
}

func (repository *SQLRepository) ExecuteFinalize(ctx context.Context, operation func(FinalizeTransaction) error) error {
	if err := repository.configured(); err != nil {
		return err
	}
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := operation(&sqlFinalizeTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (repository *SQLRepository) configured() error {
	if repository == nil || repository.db == nil {
		return errors.New("recording session database is not configured")
	}
	return nil
}

type sqlFinalizeTransaction struct{ tx pgx.Tx }

func (transaction *sqlFinalizeTransaction) LockSession(ctx context.Context, sessionID string, userID string) (Session, bool, error) {
	var session Session
	err := transaction.tx.QueryRow(ctx, `
		SELECT id, user_id, topic, duration, timestamp, practice_type,
		       photo_data_url, photo_object, audio_extension, chunk_count,
		       status, recording_id
		FROM recording_upload_sessions
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, sessionID, userID).Scan(sessionDestinations(&session)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	return session, err == nil, err
}

func (transaction *sqlFinalizeTransaction) LockQuota(ctx context.Context, userID string, now time.Time) (quota.RecordingQuota, error) {
	return quota.LockRecordingQuota(ctx, transaction.tx, userID, now)
}

func (transaction *sqlFinalizeTransaction) InsertRecording(ctx context.Context, command FinalizeCommand) (Recording, error) {
	var created Recording
	err := transaction.tx.QueryRow(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript,
		   suggestions, practice_type, audio_data_url, photo_data_url, photo_object,
		   status, processing_stage, processing_job_id)
		VALUES
		  ($1, $2, $3, $4, $5, '', '', '[]'::jsonb, $6, $7, $8, $9,
		   'processing', 'transcribing', $10)
		RETURNING id, topic, duration, timestamp, status, transcript,
		          corrected_transcript, suggestions, processing_stage, practice_type,
		          audio_data_url, photo_data_url, photo_object, processing_error,
		          shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at`,
		command.RecordingID, command.Session.UserID, command.Session.Topic,
		command.Duration, command.Timestamp, command.Session.PracticeType, command.AudioURL,
		command.Session.PhotoDataURL, command.Session.PhotoObject, command.JobID,
	).Scan(recordingDestinations(&created)...)
	return created, err
}

func (transaction *sqlFinalizeTransaction) MarkFinalized(ctx context.Context, sessionID string, userID string, recordingID string) (bool, error) {
	result, err := transaction.tx.Exec(ctx, `
		UPDATE recording_upload_sessions
		SET status = 'finalized', recording_id = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND status = 'open'`, sessionID, userID, recordingID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (transaction *sqlFinalizeTransaction) EnqueueProcessing(ctx context.Context, command FinalizeCommand) error {
	return workqueue.Enqueue(ctx, transaction.tx, workqueue.NewJob{
		ID: command.JobID, Kind: workqueue.KindRecordingProcess,
		ResourceID: command.RecordingID, IdempotencyKey: "recording:" + command.JobID,
		MaxAttempts: 3,
	})
}

const recordingSelect = `
	SELECT id, topic, duration, timestamp, status, transcript,
	       corrected_transcript, suggestions, processing_stage, practice_type,
	       audio_data_url, photo_data_url, photo_object, processing_error,
	       shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at
	FROM recordings`

func sessionDestinations(session *Session) []any {
	return []any{
		&session.ID, &session.UserID, &session.Topic, &session.Duration,
		&session.Timestamp, &session.PracticeType, &session.PhotoDataURL,
		&session.PhotoObject, &session.AudioExtension, &session.ChunkCount,
		&session.Status, &session.RecordingID,
	}
}

func recordingDestinations(recording *Recording) []any {
	return []any{
		&recording.ID, &recording.Topic, &recording.Duration, &recording.Timestamp,
		&recording.Status, &recording.Transcript, &recording.CorrectedTranscript,
		&recording.SuggestionsJSON, &recording.ProcessingStage, &recording.PracticeType,
		&recording.AudioDataURL, &recording.PhotoDataURL, &recording.PhotoObject,
		&recording.ProcessingError, &recording.ShadowingStatus,
		&recording.ShadowingAudioURL, &recording.ShadowingError,
		&recording.ShadowingUpdatedAt,
	}
}

var _ Repository = (*SQLRepository)(nil)
