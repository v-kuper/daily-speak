package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/logging"
)

func (s *Server) routeInterviewV1(w http.ResponseWriter, r *http.Request, path string) bool {
	if path == "/api/v1/interviews" {
		if r.Method == http.MethodPost {
			s.handleCreateInterviewV1(w, r)
		} else {
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return true
	}
	if !strings.HasPrefix(path, "/api/v1/interviews/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/interviews/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Interview not found")
		return true
	}
	id := parts[0]
	switch {
	case len(parts) == 4 && parts[1] == "questions" && parts[3] == "audio":
		index, err := strconv.Atoi(parts[2])
		if err != nil || index < 1 {
			s.writeInterviewError(w, r, interview.ErrInvalid)
		} else {
			s.handleQuestionAudioV1(w, r, interview.QuestionAudioInput{SessionID: id, Index: index})
		}
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleGetInterviewV1(w, r, id)
	case len(parts) == 2 && parts[1] == "start" && r.Method == http.MethodPost:
		s.handleStartInterviewV1(w, r, id)
	case len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost:
		s.handleCancelInterviewV1(w, r, id)
	case len(parts) == 2 && parts[1] == "advance" && r.Method == http.MethodPost:
		s.handleAdvanceInterviewV1(w, r, id)
	case len(parts) == 2 && parts[1] == "finalize" && r.Method == http.MethodPost:
		s.handleFinalizeInterviewV1(w, r, id)
	case len(parts) == 2 && parts[1] == "transcription-token" && r.Method == http.MethodPost:
		s.handleInterviewTranscriptionTokenV1(w, r, id)
	case len(parts) == 2 && parts[1] == "question-speech-token" && r.Method == http.MethodPost:
		s.handleInterviewQuestionSpeechTokenV1(w, r, id)
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "audio" && r.Method == http.MethodPost:
		seq, err := strconv.Atoi(parts[2])
		if err != nil || seq < 1 {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Turn sequence is invalid")
		} else {
			s.handleAttachInterviewAudioV1(w, r, id, seq)
		}
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "transcript" && r.Method == http.MethodPost:
		seq, err := strconv.Atoi(parts[2])
		if err != nil || seq < 1 {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Turn sequence is invalid")
		} else {
			s.handleSaveInterviewTranscriptV1(w, r, id, seq)
		}
	case len(parts) == 4 && parts[1] == "turns" && parts[3] == "skip" && r.Method == http.MethodPost:
		seq, err := strconv.Atoi(parts[2])
		if err != nil || seq < 1 {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Turn sequence is invalid")
		} else {
			s.handleSkipInterviewTurnV1(w, r, id, seq)
		}
	case len(parts) == 1 || len(parts) == 2 || len(parts) == 4:
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	default:
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Interview not found")
	}
	return true
}

func (s *Server) handleInterviewTranscriptionTokenV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	credential, err := s.interviewService.RealtimeTranscriptionCredential(r.Context(), identity.PrincipalID, id)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, credential)
}

func (s *Server) handleInterviewQuestionSpeechTokenV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	credential, err := s.interviewService.QuestionSpeechCredential(r.Context(), identity.PrincipalID, id)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, credential)
}

func (s *Server) interviewReady(w http.ResponseWriter, r *http.Request) bool {
	if s.interviewService != nil {
		return true
	}
	writeV1Error(w, r, http.StatusServiceUnavailable, "interview_unavailable", "Interview service is unavailable")
	return false
}

func (s *Server) handleCreateInterviewV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		Topic           string   `json:"topic"`
		OpeningQuestion string   `json:"openingQuestion"`
		EnglishLevel    string   `json:"englishLevel"`
		Interests       []string `json:"interests"`
		IdempotencyKey  string   `json:"idempotencyKey"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	userID := ""
	if identity.Kind == "user" {
		if identity.User == nil || strings.TrimSpace(identity.User.ID) == "" {
			writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Authenticated account identity is incomplete")
			return
		}
		userID = strings.TrimSpace(identity.User.ID)
	}
	created, err := s.interviewService.Create(r.Context(), interview.CreateInput{
		OwnerPrincipalID: identity.PrincipalID, OwnerKind: identity.Kind, UserID: userID,
		Topic: payload.Topic, OpeningQuestion: payload.OpeningQuestion,
		EnglishLevel: payload.EnglishLevel, Interests: payload.Interests,
		IdempotencyKey: firstInterviewKey(payload.IdempotencyKey, r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/interviews/"+created.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"interview": created})
}

func firstInterviewKey(bodyKey, headerKey string) string {
	if strings.TrimSpace(bodyKey) != "" {
		return strings.TrimSpace(bodyKey)
	}
	return strings.TrimSpace(headerKey)
}

func (s *Server) handleGetInterviewV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	session, err := s.interviewService.Get(r.Context(), identity.PrincipalID, id)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleStartInterviewV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	session, err := s.interviewService.Start(r.Context(), identity.PrincipalID, id)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleCancelInterviewV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	session, err := s.interviewService.Cancel(r.Context(), identity.PrincipalID, id)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleAdvanceInterviewV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		IdempotencyKey  string `json:"idempotencyKey"`
		CurrentTurnSeq  int    `json:"currentTurnSeq"`
		NextCandidateID string `json:"nextCandidateId"`
		AtMs            int    `json:"atMs"`
		SkipCurrent     bool   `json:"skipCurrent"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	session, err := s.interviewService.Advance(r.Context(), interview.AdvanceInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: id, IdempotencyKey: payload.IdempotencyKey,
		CurrentTurnSeq: payload.CurrentTurnSeq, NextCandidateID: payload.NextCandidateID,
		AtMs: payload.AtMs, SkipCurrent: payload.SkipCurrent,
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	if len(session.Turns) > 0 {
		last := session.Turns[len(session.Turns)-1]
		logging.ForRequest("api.v1.interview", r).Info("advance.completed", map[string]any{
			"questionSource": last.QuestionSource, "turnSeq": last.Seq,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleAttachInterviewAudioV1(w http.ResponseWriter, r *http.Request, id string, seq int) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		AudioAssetID   string `json:"audioAssetId"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	session, err := s.interviewService.AttachAudio(r.Context(), interview.AttachAudioInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: id, TurnSeq: seq,
		AudioAssetID: payload.AudioAssetID, IdempotencyKey: payload.IdempotencyKey,
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"interview": session})
}

func (s *Server) handleSaveInterviewTranscriptV1(w http.ResponseWriter, r *http.Request, id string, seq int) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		IdempotencyKey string `json:"idempotencyKey"`
		Text           string `json:"text"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	session, err := s.interviewService.SaveTurnTranscript(r.Context(), interview.SaveTurnTranscriptInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: id, TurnSeq: seq,
		IdempotencyKey: payload.IdempotencyKey, Transcript: payload.Text,
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleSkipInterviewTurnV1(w http.ResponseWriter, r *http.Request, id string, seq int) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		IdempotencyKey string `json:"idempotencyKey"`
		AtMs           int    `json:"atMs"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	session, err := s.interviewService.SkipTurn(r.Context(), interview.SkipTurnInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: id, TurnSeq: seq,
		IdempotencyKey: payload.IdempotencyKey, AtMs: payload.AtMs,
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) handleFinalizeInterviewV1(w http.ResponseWriter, r *http.Request, id string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok || !s.interviewReady(w, r) {
		return
	}
	var payload struct {
		IdempotencyKey string `json:"idempotencyKey"`
		EndedAtMs      int    `json:"endedAtMs"`
		RecordingID    string `json:"recordingId"`
		GuestPreviewID string `json:"guestPreviewId"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	if identity.Kind == "guest" && payload.RecordingID != "" || identity.Kind == "user" && payload.GuestPreviewID != "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Final resource type does not match identity")
		return
	}
	session, err := s.interviewService.Finalize(r.Context(), interview.FinalizeInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: id, IdempotencyKey: payload.IdempotencyKey,
		EndedAtMs: payload.EndedAtMs, RecordingID: payload.RecordingID, GuestPreviewID: payload.GuestPreviewID,
	})
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interview": session})
}

func (s *Server) writeInterviewError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, interview.ErrInvalid):
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Interview request is invalid")
	case errors.Is(err, interview.ErrNotFound):
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Interview or turn not found")
	case errors.Is(err, interview.ErrDurationLimit):
		writeV1Error(w, r, http.StatusForbidden, "quota_exceeded", "Interview duration limit is exhausted")
	case errors.Is(err, interview.ErrQuota):
		writeV1Error(w, r, http.StatusForbidden, "quota_exceeded", "This guest identity has already used its single interview preview")
	case errors.Is(err, interview.ErrNotReady):
		writeV1Error(w, r, http.StatusConflict, "interview_not_ready", "A next question is not ready")
	case errors.Is(err, interview.ErrUnavailable):
		writeV1Error(w, r, http.StatusServiceUnavailable, "interview_transcription_unavailable", "Live transcription is unavailable")
	case errors.Is(err, interview.ErrSpeechUnavailable):
		writeV1Error(w, r, http.StatusServiceUnavailable, "interview_speech_unavailable", "Question audio is unavailable")
	case errors.Is(err, interview.ErrConflict):
		writeV1Error(w, r, http.StatusConflict, "interview_conflict", "Interview state conflicts with this request")
	default:
		logging.ForRequest("api.v1.interview", r).Error("interview.failed", logging.ErrorMeta(err))
		writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Interview request failed")
	}
}
