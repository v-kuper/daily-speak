package httpapi

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/transcription"
)

const recordingProcessingTimeout = 30 * time.Minute

func (s *Server) processRecordingInBackground(recordingID string, userID string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string) {
	ctx, cancel := context.WithTimeout(context.Background(), recordingProcessingTimeout)
	s.registerRecordingProcessing(recordingID, cancel)
	go func() {
		defer cancel()
		defer s.unregisterRecordingProcessing(recordingID)
		logger := logging.ForBackground("api.recordings.process")
		if err := s.processSavedRecording(ctx, recordingID, userID, audioPath, topic, practiceType, photoObject, englishLevel, logger); err != nil {
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

func (s *Server) registerRecordingProcessing(recordingID string, cancel context.CancelFunc) {
	s.recordingProcessingMu.Lock()
	previous := s.recordingProcessingCancels[recordingID]
	s.recordingProcessingCancels[recordingID] = cancel
	s.recordingProcessingMu.Unlock()
	if previous != nil {
		previous()
	}
}

func (s *Server) unregisterRecordingProcessing(recordingID string) {
	s.recordingProcessingMu.Lock()
	delete(s.recordingProcessingCancels, recordingID)
	s.recordingProcessingMu.Unlock()
}

func (s *Server) cancelRecordingProcessing(recordingID string) {
	s.recordingProcessingMu.Lock()
	cancel := s.recordingProcessingCancels[recordingID]
	delete(s.recordingProcessingCancels, recordingID)
	s.recordingProcessingMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) processSavedRecording(ctx context.Context, recordingID string, userID string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) error {
	interestRows, err := s.db.Query(ctx, `
		SELECT interest_id
		FROM user_interests
		WHERE user_id = $1
		ORDER BY created_at ASC`, userID)
	if err != nil {
		return err
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
