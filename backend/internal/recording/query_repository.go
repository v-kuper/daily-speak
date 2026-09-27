package recording

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type SQLQueryRepository struct{ db *db.DB }

func NewSQLQueryRepository(database *db.DB) *SQLQueryRepository {
	return &SQLQueryRepository{db: database}
}

func (repository *SQLQueryRepository) Find(ctx context.Context, userID string, recordingID string) (Record, bool, error) {
	if repository == nil || repository.db == nil {
		return Record{}, false, errors.New("recording database is not configured")
	}
	return findRecord(ctx, repository.db, userID, recordingID)
}

func (repository *SQLQueryRepository) List(ctx context.Context, userID string, options ListOptions) ([]Record, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("recording database is not configured")
	}
	var cursor any
	if options.BeforeTimestamp != nil {
		cursor = options.BeforeTimestamp.UTC()
	}
	rows, err := repository.db.Query(ctx, `
		SELECT id, topic, duration, timestamp, status, transcript, corrected_transcript,
		       suggestions, processing_stage, practice_type, audio_data_url,
		       photo_data_url, photo_object, processing_error, shadowing_status,
		       shadowing_audio_url, shadowing_error, shadowing_updated_at,
		       audio_asset_id, photo_asset_id, shadowing_asset_id
		FROM recordings
		WHERE user_id = $1
		  AND ($2::timestamptz IS NULL OR (timestamp, id) < ($2::timestamptz, $3::text))
		ORDER BY timestamp DESC, id DESC
		LIMIT NULLIF($4, 0)`, userID, cursor, options.BeforeID, options.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		var record Record
		if err := rows.Scan(recordDestinations(&record)...); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (repository *SQLQueryRepository) OwnsLegacyShadowing(ctx context.Context, userID string, publicURL string) (bool, error) {
	if repository == nil || repository.db == nil {
		return false, errors.New("recording database is not configured")
	}
	var owned bool
	err := repository.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM recordings
			WHERE user_id = $1 AND shadowing_audio_url = $2
		)`, userID, publicURL).Scan(&owned)
	return owned, err
}

type recordQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findRecord(ctx context.Context, querier recordQuerier, userID string, recordingID string) (Record, bool, error) {
	var record Record
	err := querier.QueryRow(ctx, recordSelectSQL, recordingID, userID).Scan(recordDestinations(&record)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	return record, err == nil, err
}

const recordSelectSQL = `
	SELECT id, topic, duration, timestamp, status, transcript, corrected_transcript,
	       suggestions, processing_stage, practice_type, audio_data_url,
	       photo_data_url, photo_object, processing_error, shadowing_status,
	       shadowing_audio_url, shadowing_error, shadowing_updated_at,
	       audio_asset_id, photo_asset_id, shadowing_asset_id
	FROM recordings
	WHERE id = $1 AND user_id = $2
	LIMIT 1`

func recordDestinations(record *Record) []any {
	return []any{
		&record.ID, &record.Topic, &record.Duration, &record.Timestamp,
		&record.Status, &record.Transcript, &record.CorrectedTranscript,
		&record.SuggestionsJSON, &record.ProcessingStage, &record.PracticeType,
		&record.AudioDataURL, &record.PhotoDataURL, &record.PhotoObject,
		&record.ProcessingError, &record.ShadowingStatus,
		&record.ShadowingAudioURL, &record.ShadowingError,
		&record.ShadowingUpdatedAt, &record.AudioAssetID, &record.PhotoAssetID,
		&record.ShadowingAssetID,
	}
}

var _ RecordRepository = (*SQLQueryRepository)(nil)
