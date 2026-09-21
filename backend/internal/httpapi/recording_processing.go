package httpapi

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/transcription"
	"github.com/google/uuid"
)

const recordingProcessingTimeout = 30 * time.Minute

func (s *Server) processRecordingInBackground(recordingID string, userID string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string) {
	s.startRecordingProcessingJob(recordingID, func(ctx context.Context, logger logging.Logger) error {
		return s.processSavedRecording(ctx, recordingID, userID, audioPath, topic, practiceType, photoObject, englishLevel, logger)
	})
}

func (s *Server) startRecordingProcessingJob(recordingID string, run func(context.Context, logging.Logger) error) {
	ctx, cancel := context.WithTimeout(context.Background(), recordingProcessingTimeout)
	jobID := uuid.NewString()
	s.registerRecordingProcessing(recordingID, recordingProcessingJob{id: jobID, cancel: cancel})
	go func() {
		defer cancel()
		defer s.unregisterRecordingProcessing(recordingID, jobID)
		logger := logging.ForBackground("api.recordings.process")
		if err := run(ctx, logger); err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				logger.Info("recording.processing_cancelled", map[string]any{"recordingId": recordingID})
				return
			}
			logger.Error("recording.processing_failed", logging.ErrorMeta(err))
			_, _ = s.db.Exec(context.Background(), `
				UPDATE recordings
				SET status = 'failed', processing_error = $2
				WHERE id = $1`, recordingID, truncateRunes(err.Error(), 500))
		}
	}()
}

func (s *Server) registerRecordingProcessing(recordingID string, job recordingProcessingJob) {
	s.recordingProcessingMu.Lock()
	previous := s.recordingProcessingJobs[recordingID]
	s.recordingProcessingJobs[recordingID] = job
	s.recordingProcessingMu.Unlock()
	if previous.cancel != nil {
		previous.cancel()
	}
}

func (s *Server) unregisterRecordingProcessing(recordingID string, jobID string) {
	s.recordingProcessingMu.Lock()
	if current := s.recordingProcessingJobs[recordingID]; current.id == jobID {
		delete(s.recordingProcessingJobs, recordingID)
	}
	s.recordingProcessingMu.Unlock()
}

func (s *Server) cancelRecordingProcessing(recordingID string) {
	s.recordingProcessingMu.Lock()
	job := s.recordingProcessingJobs[recordingID]
	delete(s.recordingProcessingJobs, recordingID)
	s.recordingProcessingMu.Unlock()
	if job.cancel != nil {
		job.cancel()
	}
}

func (s *Server) processSavedRecording(ctx context.Context, recordingID string, userID string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) error {
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
	if _, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET transcript = $2,
		    processing_stage = 'suggestions',
		    processing_error = NULL
		WHERE id = $1`, recordingID, transcript); err != nil {
		return err
	}

	return s.processRecordingSuggestions(ctx, recordingID, userID, transcript, topic, interests, practiceType, photoObject, englishLevel, logger)
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

func (s *Server) processRecordingSuggestions(ctx context.Context, recordingID string, userID string, transcript string, topic string, interests []string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) error {
	suggestions, err := s.generateRecordingSuggestions(ctx, recordingID, transcript, topic, interests, practiceType, photoObject, englishLevel, logger)
	if err != nil {
		return err
	}
	suggestionJSON := marshalSuggestions(suggestions)
	if _, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET suggestions = $2::jsonb,
		    processing_stage = 'rewriting',
		    processing_error = NULL
		WHERE id = $1`, recordingID, suggestionJSON); err != nil {
		return err
	}
	return s.processRecordingRewrite(ctx, recordingID, userID, transcript, suggestions, englishLevel, logger)
}

func (s *Server) processRecordingRewrite(ctx context.Context, recordingID string, userID string, transcript string, suggestions []suggestion, englishLevel string, logger logging.Logger) error {
	correctedTranscript, err := s.generateNaturalTranscript(ctx, transcript, suggestions, englishLevel, logger)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(ctx, `
		UPDATE recordings
		SET status = 'ready',
		    corrected_transcript = $2,
		    processing_stage = NULL,
		    processing_error = NULL
		WHERE id = $1`, recordingID, correctedTranscript); err != nil {
		return err
	}
	if _, _, scheduleErr := s.scheduleShadowing(context.Background(), userID, recordingID); scheduleErr != nil {
		logger.Warn("shadowing.schedule_failed", map[string]any{"recordingId": recordingID})
	}
	return nil
}

func (s *Server) processRecordingRetry(ctx context.Context, recordingID string, userID string, work recordingRetryWork, logger logging.Logger) error {
	switch work.Stage {
	case "transcribing":
		return s.processSavedRecording(ctx, recordingID, userID, work.AudioPath, work.Topic, work.PracticeType, work.PhotoObject, work.EnglishLevel, logger)
	case "suggestions":
		interests, err := s.recordingInterests(ctx, userID)
		if err != nil {
			return err
		}
		return s.processRecordingSuggestions(ctx, recordingID, userID, work.Transcript, work.Topic, interests, work.PracticeType, work.PhotoObject, work.EnglishLevel, logger)
	case "rewriting":
		return s.processRecordingRewrite(ctx, recordingID, userID, work.Transcript, work.Suggestions, work.EnglishLevel, logger)
	default:
		return errors.New("Recording processing cannot be retried from this stage.")
	}
}
