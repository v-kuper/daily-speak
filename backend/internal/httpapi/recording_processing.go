package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/transcription"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const recordingProcessingTimeout = 30 * time.Minute

func (s *Server) runRecordingJob(ctx context.Context, job workqueue.Job) error {
	var work recordingRetryWork
	var userID, status, stage string
	var audioURL, audioAssetID, currentJobID *string
	var suggestionJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT r.user_id, r.status, COALESCE(r.processing_stage, ''), r.processing_job_id,
		       r.audio_data_url, r.audio_asset_id, r.transcript, r.suggestions, r.topic, r.practice_type,
		       r.photo_object, u.english_level
		FROM recordings r
		JOIN users u ON u.id = r.user_id
		WHERE r.id = $1`, job.ResourceID).Scan(
		&userID, &status, &stage, &currentJobID, &audioURL, &audioAssetID, &work.Transcript,
		&suggestionJSON, &work.Topic, &work.PracticeType, &work.PhotoObject, &work.EnglishLevel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "processing" || currentJobID == nil || *currentJobID != job.ID {
		return nil
	}
	work.Stage = stage
	work.Suggestions = normalizeSuggestions(suggestionJSON, 0)
	cleanupAudio := func() {}
	if stage == "transcribing" {
		if audioAssetID != nil {
			work.AudioPath, cleanupAudio, err = s.materializeMediaAsset(ctx, *audioAssetID)
			if err != nil {
				return errors.New("recording audio is unavailable")
			}
		} else if audioURL != nil {
			work.AudioPath, err = storedUploadPath(*audioURL)
			if err != nil {
				return errors.New("recording audio is unavailable")
			}
		} else {
			return errors.New("recording audio is unavailable")
		}
	}
	defer cleanupAudio()
	logger := logging.ForBackground("worker.recordings.process")
	return s.processRecordingRetry(ctx, job.ResourceID, userID, job.ID, job.LeaseToken, work, logger)
}

func (s *Server) processSavedRecording(ctx context.Context, recordingID string, userID string, jobID string, leaseToken string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) error {
	interests, err := s.recordingInterests(ctx, userID)
	if err != nil {
		return err
	}

	transcript, err := transcription.TranscribeAudioWithLocalWhisper(ctx, audioPath)
	if err != nil {
		var typed transcription.Error
		if errors.As(err, &typed) {
			return errors.New(typed.Message)
		}
		return err
	}
	transcript = domain.NormalizeTranscript(transcript)
	if transcript == "" {
		return errors.New("Whisper returned an empty transcript. Try speaking louder or recording again.")
	}
	result, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET transcript = $2,
		    processing_stage = 'suggestions',
		    processing_error = NULL
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (
		    SELECT 1 FROM processing_jobs
		    WHERE id = $3 AND state = 'running' AND lease_token = $4
		  )`, recordingID, transcript, jobID, leaseToken)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}

	return s.processRecordingSuggestions(ctx, recordingID, userID, jobID, leaseToken, transcript, topic, interests, practiceType, photoObject, englishLevel, logger)
}

func (s *Server) recordingInterests(ctx context.Context, userID string) ([]string, error) {
	interestRows, err := s.db.Query(ctx, `
		SELECT interest_id
		FROM user_interests
		WHERE user_id = $1
		ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	interests := []string{}
	for interestRows.Next() {
		var interest string
		if err := interestRows.Scan(&interest); err == nil {
			interests = append(interests, interest)
		}
	}
	interestRows.Close()
	if len(interests) > 10 {
		interests = interests[:10]
	}
	return interests, nil
}

func (s *Server) processRecordingSuggestions(ctx context.Context, recordingID string, userID string, jobID string, leaseToken string, transcript string, topic string, interests []string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) error {
	suggestions, err := s.generateRecordingSuggestions(ctx, recordingID, transcript, topic, interests, practiceType, photoObject, englishLevel, logger)
	if err != nil {
		return err
	}
	suggestionJSON := marshalSuggestions(suggestions)
	result, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET suggestions = $2::jsonb,
		    processing_stage = 'rewriting',
		    processing_error = NULL
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (
		    SELECT 1 FROM processing_jobs
		    WHERE id = $3 AND state = 'running' AND lease_token = $4
		  )`, recordingID, suggestionJSON, jobID, leaseToken)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	return s.processRecordingRewrite(ctx, recordingID, jobID, leaseToken, transcript, suggestions, englishLevel, logger)
}

func (s *Server) processRecordingRewrite(ctx context.Context, recordingID string, jobID string, leaseToken string, transcript string, suggestions []suggestion, englishLevel string, logger logging.Logger) error {
	correctedTranscript, err := s.generateNaturalTranscript(ctx, transcript, suggestions, englishLevel, logger)
	if err != nil {
		return err
	}
	shadowingJobID := uuid.NewString()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE recordings
		SET status = 'ready',
		    corrected_transcript = $2,
		    processing_stage = NULL,
		    processing_error = NULL,
		    shadowing_status = 'processing',
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW(),
		    shadowing_attempt_id = $4
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $3
		  AND EXISTS (
		    SELECT 1 FROM processing_jobs
		    WHERE id = $3 AND state = 'running' AND lease_token = $5
		  )`, recordingID, correctedTranscript, jobID, shadowingJobID, leaseToken)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID:             shadowingJobID,
		Kind:           workqueue.KindShadowingSynthesize,
		ResourceID:     recordingID,
		IdempotencyKey: "shadowing:" + shadowingJobID,
		MaxAttempts:    4,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) processRecordingRetry(ctx context.Context, recordingID string, userID string, jobID string, leaseToken string, work recordingRetryWork, logger logging.Logger) error {
	switch work.Stage {
	case "transcribing":
		return s.processSavedRecording(ctx, recordingID, userID, jobID, leaseToken, work.AudioPath, work.Topic, work.PracticeType, work.PhotoObject, work.EnglishLevel, logger)
	case "suggestions":
		interests, err := s.recordingInterests(ctx, userID)
		if err != nil {
			return err
		}
		return s.processRecordingSuggestions(ctx, recordingID, userID, jobID, leaseToken, work.Transcript, work.Topic, interests, work.PracticeType, work.PhotoObject, work.EnglishLevel, logger)
	case "rewriting":
		return s.processRecordingRewrite(ctx, recordingID, jobID, leaseToken, work.Transcript, work.Suggestions, work.EnglishLevel, logger)
	default:
		return errors.New("recording processing cannot resume from persisted stage " + strings.TrimSpace(work.Stage))
	}
}
