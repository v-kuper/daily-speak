package recording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
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
		SELECT r.user_id, COALESCE(r.processing_stage, ''), r.audio_asset_id,
		       r.transcript, r.suggestions, r.topic, r.practice_type, r.photo_object, u.english_level,
		       EXISTS (
		         SELECT 1 FROM guest_previews p
		         WHERE p.promoted_recording_id = r.id AND p.state = 'promoted'
		       ),
		       (SELECT s.id FROM interview_sessions s WHERE s.recording_id = r.id), r.duration
		FROM recordings r
		JOIN users u ON u.id = r.user_id
		WHERE r.id = $1 AND r.status = 'processing' AND r.processing_job_id = $2`,
		job.ResourceID, job.ID,
	).Scan(
		&work.UserID, &work.Stage, &work.AudioAssetID, &work.Transcript,
		&suggestionJSON, &work.Topic, &work.PracticeType, &work.PhotoObject, &work.EnglishLevel,
		&work.PromotedGuestPreview, &work.InterviewSessionID, &work.DeclaredDuration,
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

func (r *SQLProcessingRepository) VerifyDuration(ctx context.Context, job ProcessingJob, actualSeconds int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireRecordingLease(ctx, tx, job); err != nil {
		return err
	}
	var recordingID string
	err = tx.QueryRow(ctx, `
			SELECT id
			FROM recordings
			WHERE id = $1 AND status = 'processing' AND processing_job_id = $2
			FOR UPDATE`, job.ResourceID, job.ID).Scan(&recordingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("recording is no longer active")
	}
	if err != nil {
		return err
	}
	if err := validateVerifiedDuration(actualSeconds); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE recordings SET duration = $2 WHERE id = $1`, job.ResourceID, actualSeconds); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SQLProcessingRepository) VerifyInterviewDuration(ctx context.Context, job ProcessingJob, sessionID string, actualSeconds, actualMS int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireRecordingLease(ctx, tx, job); err != nil {
		return err
	}
	var maximum int
	err = tx.QueryRow(ctx, `
			SELECT s.max_duration_seconds
			FROM recordings r JOIN interview_sessions s ON s.recording_id = r.id
			WHERE r.id = $1 AND r.status = 'processing' AND r.processing_job_id = $2 AND s.id = $3
			FOR UPDATE OF r`, job.ResourceID, job.ID, sessionID).
		Scan(&maximum)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("interview recording is no longer active")
	}
	if err != nil {
		return err
	}
	if actualSeconds < 1 || actualSeconds > maximum {
		return errors.New("interview audio exceeds its duration limit")
	}
	if _, err := tx.Exec(ctx, `UPDATE recordings SET duration = $2 WHERE id = $1`, job.ResourceID, actualSeconds); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE interview_turns t
		SET ended_at_ms = GREATEST(t.asked_at_ms + 1, $2), updated_at = NOW()
		WHERE t.session_id = $1
		  AND t.seq = (SELECT MAX(seq) FROM interview_turns WHERE session_id = $1)`,
		sessionID, actualMS); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func requireRecordingLease(ctx context.Context, tx pgx.Tx, job ProcessingJob) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT state = 'running' AND lease_token = $2 AND COALESCE(lease_expires_at > NOW(), FALSE)
		FROM processing_jobs WHERE id = $1 FOR SHARE`, job.ID, job.LeaseToken).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return workqueue.ErrLeaseLost
	}
	return err
}

func validateVerifiedDuration(actualSeconds int) error {
	if actualSeconds < 1 || actualSeconds > quota.AccountMaxSessionSeconds {
		return errors.New("recording audio exceeds its duration limit")
	}
	return nil
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

func (r *SQLProcessingRepository) LoadInterviewTurns(ctx context.Context, sessionID string) ([]InterviewTurn, error) {
	rows, err := r.db.Query(ctx, `
		SELECT seq, question, asked_at_ms, ended_at_ms, provisional_transcript
		FROM interview_turns WHERE session_id = $1 ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []InterviewTurn
	for rows.Next() {
		var turn InterviewTurn
		if err := rows.Scan(&turn.Sequence, &turn.Question, &turn.AskedAtMS, &turn.EndedAtMS, &turn.Provisional); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (r *SQLProcessingRepository) SaveInterviewTranscript(ctx context.Context, job ProcessingJob, transcript string, sessionID string, answers map[int]string) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE recordings
		SET transcript = $2, processing_stage = 'suggestions', processing_error = NULL
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $3 AND state = 'running' AND lease_token = $4)`,
		job.ResourceID, transcript, job.ID, job.LeaseToken)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	for sequence, answer := range answers {
		result, err := tx.Exec(ctx, `
			UPDATE interview_turns t
			SET final_transcript = $3, updated_at = NOW()
			FROM interview_sessions s
			WHERE t.session_id = s.id AND s.id = $1 AND s.recording_id = $2
			  AND t.seq = $4`, sessionID, job.ResourceID, answer, sequence)
		if err != nil {
			return false, err
		}
		if result.RowsAffected() != 1 {
			return false, errors.New("interview timeline changed during recording processing")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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
