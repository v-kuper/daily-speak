package httpapi

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/transcription"
)

func (s *Server) processRecordingInBackground(recordingID string, userID string, audioPath string, topic string, practiceType string, photoObject *string, englishLevel string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		logger := logging.ForBackground("api.recordings.process")
		if err := s.processSavedRecording(ctx, recordingID, userID, audioPath, topic, practiceType, photoObject, englishLevel, logger); err != nil {
			logger.Error("recording.processing_failed", logging.ErrorMeta(err))
			_, _ = s.db.Exec(context.Background(), `
				UPDATE recordings
				SET status = 'failed', processing_error = $2
				WHERE id = $1`, recordingID, truncateRunes(err.Error(), 500))
		}
	}()
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

	suggestions, err := s.generateRecordingSuggestions(ctx, transcript, topic, interests, practiceType, photoObject, englishLevel, logger)
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
	_, err = s.db.Exec(ctx, `
		UPDATE recordings
		SET status = 'ready',
		    corrected_transcript = $2,
		    processing_stage = NULL,
		    processing_error = NULL
		WHERE id = $1`, recordingID, correctedTranscript)
	return err
}
