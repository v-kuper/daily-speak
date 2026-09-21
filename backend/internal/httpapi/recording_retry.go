package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"github.com/jackc/pgx/v5"
)

var errRecordingRetryUnavailable = errors.New("This recording cannot be retried from its current stage.")

type recordingRetryWork struct {
	Stage        string
	AudioPath    string
	Transcript   string
	Suggestions  []suggestion
	Topic        string
	PracticeType string
	PhotoObject  *string
	EnglishLevel string
}

func recordingAfterRetryClaim(recording recordingResponse, startedAt time.Time) recordingResponse {
	updated := recording
	updated.Status = "processing"
	updated.ProcessingError = nil
	updated.CorrectedTranscript = ""
	updated.ShadowingStatus = "pending"
	updated.ShadowingAudioURL = nil
	updated.ShadowingError = nil
	updated.ShadowingUpdatedAt = startedAt.UTC().Format(time.RFC3339Nano)
	if updated.ProcessingStage != nil {
		switch *updated.ProcessingStage {
		case "transcribing":
			updated.Transcript = ""
			updated.Suggestions = []suggestion{}
		case "suggestions":
			updated.Suggestions = []suggestion{}
		}
	}
	return updated
}

func recordingRetryWorkFor(recording recordingResponse, englishLevel string) (recordingRetryWork, error) {
	if recording.Status != "failed" || recording.ProcessingStage == nil {
		return recordingRetryWork{}, errRecordingRetryUnavailable
	}
	work := recordingRetryWork{
		Stage:        *recording.ProcessingStage,
		Transcript:   recording.Transcript,
		Suggestions:  recording.Suggestions,
		Topic:        recording.Topic,
		PracticeType: recording.PracticeType,
		PhotoObject:  recording.PhotoObject,
		EnglishLevel: englishLevel,
	}
	switch work.Stage {
	case "transcribing":
		if recording.AudioDataURL == nil {
			return recordingRetryWork{}, errRecordingRetryUnavailable
		}
		audioPath, err := storedUploadPath(*recording.AudioDataURL)
		if err != nil {
			return recordingRetryWork{}, errRecordingRetryUnavailable
		}
		work.AudioPath = audioPath
	case "suggestions", "rewriting":
		if strings.TrimSpace(work.Transcript) == "" {
			return recordingRetryWork{}, errRecordingRetryUnavailable
		}
	default:
		return recordingRetryWork{}, errRecordingRetryUnavailable
	}
	return work, nil
}

func (s *Server) routeRecordingRetryPath(w http.ResponseWriter, r *http.Request, relativePath string) {
	parts := strings.Split(strings.Trim(relativePath, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "retry" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	s.handleRetryRecording(w, r, pathUnescape(parts[0]))
}

func (s *Server) handleRetryRecording(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.recordings.retry", r)
	user, ok := s.authorizedUser(w, r, "api.recordings.retry")
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}

	recording, scheduled, err := s.scheduleRecordingRetry(r.Context(), user.ID, recordingID, user.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if errors.Is(err, errRecordingRetryUnavailable) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": errRecordingRetryUnavailable.Error()})
		return
	}
	if err != nil {
		logger.Error("recording.retry_schedule_failed", map[string]any{"recordingId": recordingID})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retry recording processing."})
		return
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"recordingId": recordingID,
		"scheduled":   scheduled,
	})
	writeJSON(w, http.StatusOK, map[string]any{"recording": recording, "scheduled": scheduled})
}

func (s *Server) scheduleRecordingRetry(ctx context.Context, userID string, recordingID string, englishLevel string) (recordingResponse, bool, error) {
	recording, err := s.recordingForUser(ctx, userID, recordingID)
	if err != nil {
		return recordingResponse{}, false, err
	}
	if recording.Status == "processing" {
		return recording, false, nil
	}
	work, err := recordingRetryWorkFor(recording, englishLevel)
	if err != nil {
		return recordingResponse{}, false, err
	}

	startedAt := time.Now().UTC()
	result, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET status = 'processing',
		    processing_error = NULL,
		    transcript = CASE WHEN processing_stage = 'transcribing' THEN '' ELSE transcript END,
		    suggestions = CASE WHEN processing_stage IN ('transcribing', 'suggestions') THEN '[]'::jsonb ELSE suggestions END,
		    corrected_transcript = '',
		    shadowing_status = 'pending',
		    shadowing_audio_url = NULL,
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW(),
		    shadowing_attempt_id = NULL
		WHERE id = $1 AND user_id = $2 AND status = 'failed'`, recordingID, userID)
	if err != nil {
		return recordingResponse{}, false, err
	}
	if result.RowsAffected() == 0 {
		current, loadErr := s.recordingForUser(ctx, userID, recordingID)
		if loadErr != nil {
			return recordingResponse{}, false, loadErr
		}
		if current.Status == "processing" {
			return current, false, nil
		}
		return recordingResponse{}, false, errRecordingRetryUnavailable
	}

	s.startRecordingProcessingJob(recordingID, func(jobContext context.Context, logger logging.Logger) error {
		return s.processRecordingRetry(jobContext, recordingID, userID, work, logger)
	})
	return recordingAfterRetryClaim(recording, startedAt), true, nil
}
