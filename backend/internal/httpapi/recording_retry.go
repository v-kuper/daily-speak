package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
)

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

	result, err := s.recordingRetryService.Retry(r.Context(), user.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if errors.Is(err, recording.ErrRetryUnavailable) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": recording.ErrRetryUnavailable.Error()})
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
		"scheduled":   result.Scheduled,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"recording": recordingResponseFromRecord(result.Record),
		"scheduled": result.Scheduled,
	})
}
