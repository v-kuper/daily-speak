package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) handleDeleteRecordingV1(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.v1.recordings.delete", r)
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(pathUnescape(recordingID))
	if recordingID == "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_recording_id", "Recording ID is required")
		return
	}

	result, err := s.recordingDeleter.Delete(r.Context(), identity.User.ID, identity.User.IsSubscriber, recordingID)
	if errors.Is(err, recording.ErrDeleteNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if err != nil {
		logger.Error("recording.delete_failed", logging.ErrorMeta(err))
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to delete recording")
		return
	}
	if result.QuotaRefreshFailed {
		logger.Warn("recording.delete_quota_refresh_failed", map[string]any{"recordingId": result.RecordingID})
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"userId":      identity.User.ID,
		"recordingId": result.RecordingID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deletedRecordingId": result.RecordingID, "quota": result.Quota})
}
