package httpapi

import (
	"errors"
	"net/http"

	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) handleRetryStrengthsV1(w http.ResponseWriter, r *http.Request, recordingID string) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	scheduled, err := s.recordingStrengthsService.Retry(r.Context(), identity.User.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if errors.Is(err, recording.ErrStrengthsUnavailable) {
		writeV1Error(w, r, http.StatusConflict, "strengths_not_ready", recording.ErrStrengthsUnavailable.Error())
		return
	}
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to schedule good examples")
		return
	}
	record, err := s.recordingReader.Get(r.Context(), identity.User.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to load recording")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording": recordingV1ResponseFromRecord(record), "scheduled": scheduled})
}
