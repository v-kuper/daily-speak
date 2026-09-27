package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
	"github.com/jackc/pgx/v5"
)

var errShadowingTranscriptUnavailable = shadowing.ErrTranscriptUnavailable

func (s *Server) routeShadowingPath(w http.ResponseWriter, r *http.Request, relativePath string) {
	parts := strings.Split(strings.Trim(relativePath, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "shadowing" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	s.handleGenerateShadowing(w, r, pathUnescape(parts[0]))
}

func (s *Server) handleGenerateShadowing(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.recordings.shadowing", r)
	user, ok := s.authorizedUser(w, r, "api.recordings.shadowing")
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}
	record, scheduled, err := s.scheduleShadowing(r.Context(), user.ID, recordingID)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, recording.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if errors.Is(err, errShadowingTranscriptUnavailable) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "The natural transcript is not ready yet."})
		return
	}
	if err != nil {
		logger.Error("shadowing.schedule_failed", map[string]any{"recordingId": recordingID})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate pronunciation audio."})
		return
	}
	logger.Info("request.success", map[string]any{"status": http.StatusOK, "durationMs": logging.ElapsedMs(started), "recordingId": recordingID, "scheduled": scheduled})
	writeJSON(w, http.StatusOK, map[string]any{"recording": record})
}

func (s *Server) scheduleShadowing(ctx context.Context, userID, recordingID string) (recordingResponse, bool, error) {
	scheduled, err := s.shadowingStore.Schedule(ctx, userID, recordingID)
	if err != nil {
		return recordingResponse{}, false, err
	}
	recording, err := s.recordingForUser(ctx, userID, recordingID)
	return recording, scheduled, err
}
