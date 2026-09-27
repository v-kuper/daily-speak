package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) handleDeleteRecording(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.recordings.delete", r)
	user, ok := s.authorizedUser(w, r, "api.recordings.delete")
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(pathUnescape(recordingID))
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}

	result, err := s.recordingDeleter.Delete(r.Context(), user.ID, user.IsSubscriber, recordingID)
	if errors.Is(err, recording.ErrDeleteNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		logger.Error("recording.delete_failed", logging.ErrorMeta(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}
	if result.QuotaRefreshFailed {
		logger.Warn("recording.delete_quota_refresh_failed", map[string]any{"recordingId": result.RecordingID})
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"userId":      user.ID,
		"recordingId": result.RecordingID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deletedRecordingId": result.RecordingID, "quota": result.Quota})
}
