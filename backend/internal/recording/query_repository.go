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
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Record{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return Record{}, false, err
	}
	record, found, err := findRecord(ctx, tx, userID, recordingID)
	if err != nil || !found {
		return record, found, err
	}
	record.InterviewTurns, err = repository.interviewTurns(ctx, tx, record.ID)
	if err != nil {
		return Record{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

type interviewTurnQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (repository *SQLQueryRepository) interviewTurns(ctx context.Context, querier interviewTurnQuerier, recordingID string) ([]InterviewTurn, error) {
	rows, err := querier.Query(ctx, `
		SELECT t.seq, t.question, t.asked_at_ms, t.ended_at_ms,
		       t.provisional_transcript, t.final_transcript,
		       COALESCE(t.corrected_answer_text, '')
		FROM interview_turns t
		JOIN interview_sessions s ON s.id = t.session_id
		WHERE s.recording_id = $1 AND NOT t.skipped
		ORDER BY t.seq`, recordingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []InterviewTurn
	for rows.Next() {
		var turn InterviewTurn
		if err := rows.Scan(&turn.Sequence, &turn.Question, &turn.AskedAtMS,
			&turn.EndedAtMS, &turn.Provisional, &turn.FinalText,
			&turn.CorrectedAnswerText); err != nil {
			return nil, err
		}
		turn.ResolveAnswer()
		turns = append(turns, turn)
	}
	return turns, rows.Err()
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
		       suggestions, processing_stage, practice_type, photo_object,
		       processing_error, shadowing_status, shadowing_error, shadowing_updated_at,
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
	       suggestions, processing_stage, practice_type, photo_object,
	       processing_error, shadowing_status, shadowing_error, shadowing_updated_at,
	       audio_asset_id, photo_asset_id, shadowing_asset_id
	FROM recordings
	WHERE id = $1 AND user_id = $2
	LIMIT 1`

func recordDestinations(record *Record) []any {
	return []any{
		&record.ID, &record.Topic, &record.Duration, &record.Timestamp,
		&record.Status, &record.Transcript, &record.CorrectedTranscript,
		&record.SuggestionsJSON, &record.ProcessingStage, &record.PracticeType,
		&record.PhotoObject, &record.ProcessingError, &record.ShadowingStatus,
		&record.ShadowingError,
		&record.ShadowingUpdatedAt, &record.AudioAssetID, &record.PhotoAssetID,
		&record.ShadowingAssetID,
	}
}

var _ RecordRepository = (*SQLQueryRepository)(nil)
