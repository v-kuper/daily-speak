package recording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type SQLProcessingRepository struct {
	db *db.DB
}

func NewSQLProcessingRepository(database *db.DB) *SQLProcessingRepository {
	return &SQLProcessingRepository{db: database}
}

func (r *SQLProcessingRepository) LoadProcessingWork(ctx context.Context, job ProcessingJob) (ProcessingWork, bool, error) {
	if r == nil || r.db == nil {
		return ProcessingWork{}, false, errors.New("recording database is not configured")
	}
	var work ProcessingWork
	var suggestionJSON []byte
	err := r.db.QueryRow(ctx, `
		SELECT r.user_id, COALESCE(r.processing_stage, ''), r.audio_data_url, r.audio_asset_id,
		       r.transcript, r.suggestions, r.topic, r.practice_type, r.photo_object, u.english_level,
		       EXISTS (
		         SELECT 1 FROM guest_previews p
		         WHERE p.promoted_recording_id = r.id AND p.state = 'promoted'
		       )
		FROM recordings r
		JOIN users u ON u.id = r.user_id
		WHERE r.id = $1 AND r.status = 'processing' AND r.processing_job_id = $2`,
		job.ResourceID, job.ID,
	).Scan(
		&work.UserID, &work.Stage, &work.AudioURL, &work.AudioAssetID, &work.Transcript,
		&suggestionJSON, &work.Topic, &work.PracticeType, &work.PhotoObject, &work.EnglishLevel,
		&work.PromotedGuestPreview,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcessingWork{}, false, nil
	}
	if err != nil {
		return ProcessingWork{}, false, err
	}
	work.Suggestions = NormalizeSuggestions(suggestionJSON, 0)
	return work, true, nil
}

func (r *SQLProcessingRepository) UpdateVerifiedDuration(ctx context.Context, job ProcessingJob, duration int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE recordings SET duration = $2
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3`,
		job.ResourceID, duration, job.ID)
	return err
}

func (r *SQLProcessingRepository) UserInterests(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT interest_id FROM user_interests
		WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	interests := []string{}
	for rows.Next() {
		var interest string
		if err := rows.Scan(&interest); err != nil {
			return nil, err
		}
		interests = append(interests, interest)
		if len(interests) == 10 {
			break
		}
	}
	return interests, rows.Err()
}

func (r *SQLProcessingRepository) SaveTranscript(ctx context.Context, job ProcessingJob, transcript string) (bool, error) {
	result, err := r.db.Exec(ctx, `
		UPDATE recordings
		SET transcript = $2, processing_stage = 'suggestions', processing_error = NULL
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $3 AND state = 'running' AND lease_token = $4)`,
		job.ResourceID, transcript, job.ID, job.LeaseToken)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *SQLProcessingRepository) SaveSuggestions(ctx context.Context, job ProcessingJob, suggestions []Suggestion) (bool, error) {
	payload, _ := json.Marshal(withoutReferences(suggestions))
	result, err := r.db.Exec(ctx, `
		UPDATE recordings
		SET suggestions = $2::jsonb, processing_stage = 'rewriting', processing_error = NULL
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $3 AND state = 'running' AND lease_token = $4)`,
		job.ResourceID, string(payload), job.ID, job.LeaseToken)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *SQLProcessingRepository) CompleteRecording(ctx context.Context, job ProcessingJob, correctedTranscript, shadowingJobID string) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE recordings
		SET status = 'ready', corrected_transcript = $2, processing_stage = NULL,
		    processing_error = NULL, shadowing_status = 'processing', shadowing_error = NULL,
		    shadowing_updated_at = NOW(), shadowing_attempt_id = $4
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $3 AND state = 'running' AND lease_token = $5)`,
		job.ResourceID, correctedTranscript, job.ID, shadowingJobID, job.LeaseToken)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID: shadowingJobID, Kind: workqueue.KindShadowingSynthesize, ResourceID: job.ResourceID,
		IdempotencyKey: "shadowing:" + shadowingJobID, MaxAttempts: 4,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *SQLProcessingRepository) FinalizeFailure(ctx context.Context, tx pgx.Tx, jobID, recordingID, message string) error {
	_, err := tx.Exec(ctx, `
		UPDATE recordings SET status = 'failed', processing_error = $3
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $2`,
		recordingID, jobID, truncate(message, 500))
	return err
}

func withoutReferences(suggestions []Suggestion) []Suggestion {
	out := make([]Suggestion, len(suggestions))
	for index, item := range suggestions {
		out[index] = WithoutLearningReference(item)
	}
	return out
}

func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
