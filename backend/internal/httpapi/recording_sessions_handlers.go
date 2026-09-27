package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/recordingsession"
)

func (s *Server) handleCreateRecordingSession(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.post")
	if !ok {
		return
	}
	var payload struct {
		Topic        string `json:"topic"`
		Duration     int    `json:"duration"`
		Timestamp    string `json:"timestamp"`
		PracticeType string `json:"practiceType"`
		PhotoDataURL string `json:"photoDataUrl"`
		PhotoObject  string `json:"photoObject"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	sessionID, err := s.recordingSessionService.Start(r.Context(), user.ID, recordingsession.StartInput{
		Topic: payload.Topic, Duration: payload.Duration,
		Timestamp: domain.ParseTimestamp(payload.Timestamp), PracticeType: payload.PracticeType,
		PhotoDataURL: payload.PhotoDataURL, PhotoObject: payload.PhotoObject,
	})
	var validationError *recordingsession.ValidationError
	if errors.As(err, &validationError) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": validationError.Message})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start recording upload."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sessionId": sessionID, "chunkSeconds": 5})
}

func (s *Server) routeRecordingSessionPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	sessionID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	switch {
	case action == "chunks" && r.Method == http.MethodPost:
		s.handleUploadRecordingSessionChunk(w, r, sessionID)
	case action == "audio" && r.Method == http.MethodPost:
		s.handleUploadRecordingSessionAudio(w, r, sessionID)
	case action == "finish" && r.Method == http.MethodPost:
		s.handleFinishRecordingSession(w, r, sessionID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func (s *Server) handleUploadRecordingSessionChunk(w http.ResponseWriter, r *http.Request, sessionID string) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.chunks.post")
	if !ok {
		return
	}
	chunk, err := readMultipartChunkRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording chunk is invalid."})
		return
	}
	err = s.recordingSessionService.SaveChunk(
		r.Context(), user.ID, sessionID, chunk.Index, chunk.Extension, chunk.Bytes,
	)
	switch {
	case errors.Is(err, recordingsession.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording upload session not found."})
	case errors.Is(err, recordingsession.ErrFinalized):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording upload session is already finalized."})
	case errors.Is(err, recordingsession.ErrFormatChange):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording chunk format changed during upload."})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording chunk."})
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"sessionId": sessionID, "chunkIndex": chunk.Index})
	}
}

func (s *Server) handleUploadRecordingSessionAudio(w http.ResponseWriter, r *http.Request, sessionID string) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.audio.post")
	if !ok {
		return
	}
	audio, err := readMultipartFinalAudioRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording audio is invalid."})
		return
	}
	err = s.recordingSessionService.SaveFinal(r.Context(), user.ID, sessionID, audio.Extension, audio.Bytes)
	switch {
	case errors.Is(err, recordingsession.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording upload session not found."})
	case errors.Is(err, recordingsession.ErrFinalized):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording upload session is already finalized."})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording audio."})
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"sessionId": sessionID, "extension": audio.Extension})
	}
}

func (s *Server) handleFinishRecordingSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.finish.post")
	if !ok {
		return
	}
	var payload struct {
		Duration  int    `json:"duration"`
		Timestamp string `json:"timestamp"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	var timestamp *time.Time
	if strings.TrimSpace(payload.Timestamp) != "" {
		parsed := domain.ParseTimestamp(payload.Timestamp)
		timestamp = &parsed
	}
	result, err := s.recordingSessionService.Finalize(
		r.Context(), user.ID, user.IsSubscriber, sessionID,
		recordingsession.FinalizeInput{Duration: payload.Duration, Timestamp: timestamp},
	)
	var quotaViolation *recording.QuotaViolation
	switch {
	case errors.Is(err, recordingsession.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording upload session not found."})
	case errors.Is(err, recordingsession.ErrFinalized):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording upload session is already finalized."})
	case errors.Is(err, recordingsession.ErrAudioPending):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Recording audio is still uploading."})
	case errors.As(err, &quotaViolation) && quotaViolation.SubscriberLimit:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Subscribers can save recordings up to 10:00 per session."})
	case errors.As(err, &quotaViolation):
		message := "Weekly free limit exceeded. You have " + domain.FormatSeconds(quotaViolation.Remaining) +
			" left out of " + domain.FormatSeconds(domain.FreeWeeklyLimitSeconds) + " this week."
		writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to finish recording upload."})
	case !result.Created:
		writeJSON(w, http.StatusOK, map[string]any{"recording": recordingResponseFromUploadSession(result.Recording)})
	default:
		writeJSON(w, http.StatusCreated, map[string]any{
			"recording": recordingResponseFromUploadSession(result.Recording),
			"quota":     result.Quota,
		})
	}
}

func recordingResponseFromUploadSession(created recordingsession.Recording) recordingResponse {
	return recordingResponse{
		ID: created.ID, Topic: created.Topic, Duration: domain.ToNonNegativeInt(created.Duration),
		Timestamp: created.Timestamp.UTC().Format(time.RFC3339Nano), Status: normalizeRecordingStatus(created.Status),
		Transcript: created.Transcript, CorrectedTranscript: created.CorrectedTranscript,
		Suggestions:        normalizeSuggestions(created.SuggestionsJSON, 0),
		ProcessingStage:    normalizeRecordingProcessingStage(created.ProcessingStage),
		PracticeType:       domain.NormalizePracticeType(created.PracticeType),
		AudioDataURL:       normalizeOptionalAudio(created.AudioDataURL, true),
		PhotoDataURL:       normalizeOptionalPhoto(created.PhotoDataURL),
		PhotoObject:        normalizeOptionalPhotoObject(created.PhotoObject),
		ProcessingError:    normalizeOptionalProcessingError(created.ProcessingError),
		ShadowingStatus:    normalizeShadowingStatus(created.ShadowingStatus),
		ShadowingAudioURL:  normalizeOptionalShadowingAudio(created.ShadowingAudioURL),
		ShadowingError:     normalizeOptionalProcessingError(created.ShadowingError),
		ShadowingUpdatedAt: created.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
