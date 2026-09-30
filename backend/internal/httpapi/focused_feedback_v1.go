package httpapi

import (
	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/recording"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

func audioMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodPost {
		return true
	}
	writeV1Error(w, r, 405, "method_not_allowed", "Method not allowed")
	return false
}
func (s *Server) audioResponse(w http.ResponseWriter, r *http.Request, status, message string, download *media.Download, index int) {
	response := map[string]any{"status": status}
	if message != "" {
		response["error"] = message
	}
	if index > 0 {
		response["questionIndex"] = index
	}
	if download != nil {
		request := download.Request
		if download.Local {
			if s.mediaSigner == nil {
				s.writeMediaError(w, r, media.ErrStorage)
				return
			}
			request.URL = s.mediaSigner.Sign(http.MethodGet, "/api/v1/media/local/assets/"+url.PathEscape(download.Asset.ID)+"/content", nil, request.ExpiresAt)
		}
		response["asset"] = mediaAssetResponse(download.Asset)
		response["request"] = mediaRequestResponse(request)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	code := 200
	if r.Method == http.MethodPost && status == "processing" {
		code = 202
	}
	writeJSON(w, code, response)
}
func (s *Server) handleFeedbackAudioV1(w http.ResponseWriter, r *http.Request, id, attemptID, focusID string) {
	if !audioMethod(w, r) {
		return
	}
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	state, err := s.feedbackAudioService.Audio(r.Context(), recording.FeedbackAudioInput{OwnerID: identity.PrincipalID, RecordingID: id, AttemptID: attemptID, FeedbackID: focusID}, r.Method == http.MethodPost)
	if err != nil {
		s.writeFocusedError(w, r, err)
		return
	}
	s.audioResponse(w, r, state.Status, state.Error, state.Download, 0)
}
func (s *Server) handleQuestionAudioV1(w http.ResponseWriter, r *http.Request, input interview.QuestionAudioInput) {
	if !audioMethod(w, r) {
		return
	}
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	if input.RecordingID != "" && identity.Kind != "user" {
		writeV1Error(w, r, 403, "account_required", "An account is required")
		return
	}
	input.OwnerID = identity.PrincipalID
	state, err := s.questionAudioService.Audio(r.Context(), input, r.Method == http.MethodPost)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	s.audioResponse(w, r, state.Status, state.Error, state.Download, state.QuestionIndex)
}
func (s *Server) handleFeedbackReanalysisV1(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeV1Error(w, r, 405, "method_not_allowed", "Method not allowed")
		return
	}
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	var body struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decodeMediaJSON(w, r, &body) {
		return
	}
	scheduled, err := s.feedbackReanalysisService.Schedule(r.Context(), identity.PrincipalID, id, firstInterviewKey(body.IdempotencyKey, r.Header.Get("Idempotency-Key")))
	if err != nil {
		s.writeFocusedError(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"scheduled": scheduled})
}
func (s *Server) handleAnswerAttemptsV1(w http.ResponseWriter, r *http.Request, id string, seq int, attemptID string) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	if seq < 1 {
		s.writeInterviewError(w, r, interview.ErrInvalid)
		return
	}
	if attemptID != "" {
		if r.Method != http.MethodGet {
			writeV1Error(w, r, 405, "method_not_allowed", "Method not allowed")
			return
		}
		attempt, err := s.answerAttemptService.Get(r.Context(), identity.PrincipalID, id, attemptID)
		if err != nil {
			s.writeInterviewError(w, r, err)
			return
		}
		if attempt.TurnSequence != seq {
			s.writeInterviewError(w, r, interview.ErrNotFound)
			return
		}
		writeJSON(w, 200, map[string]any{"attempt": attempt})
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 20
		if value := r.URL.Query().Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				s.writeInterviewError(w, r, interview.ErrInvalid)
				return
			}
			limit = parsed
		}
		attempts, err := s.answerAttemptService.List(r.Context(), identity.PrincipalID, id, seq, limit, r.URL.Query().Get("before"))
		if err != nil {
			s.writeInterviewError(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": attempts})
	case http.MethodPost:
		var body struct {
			AudioAssetID   string `json:"audioAssetId"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if !decodeMediaJSON(w, r, &body) {
			return
		}
		attempt, err := s.answerAttemptService.Create(r.Context(), interview.CreateAttemptInput{OwnerID: identity.PrincipalID, RecordingID: id, TurnSeq: seq, AudioAssetID: body.AudioAssetID, IdempotencyKey: firstInterviewKey(body.IdempotencyKey, r.Header.Get("Idempotency-Key"))})
		if err != nil {
			s.writeInterviewError(w, r, err)
			return
		}
		writeJSON(w, 202, map[string]any{"attempt": attempt})
	default:
		writeV1Error(w, r, 405, "method_not_allowed", "Method not allowed")
	}
}
func (s *Server) writeFocusedError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, recording.ErrNotFound):
		writeV1Error(w, r, 404, "not_found", "Recording or feedback not found")
	case errors.Is(err, recording.ErrFeedbackInvalid):
		writeV1Error(w, r, 400, "invalid_request", "Feedback request is invalid")
	case errors.Is(err, recording.ErrFeedbackConflict):
		writeV1Error(w, r, 409, "feedback_not_ready", err.Error())
	case errors.Is(err, recording.ErrFeedbackUnavailable):
		writeV1Error(w, r, 409, "feedback_unavailable", err.Error())
	case errors.Is(err, media.ErrNotFound), errors.Is(err, media.ErrStorage):
		s.writeMediaError(w, r, err)
	default:
		writeV1Error(w, r, 500, "feedback_unavailable", "Feedback request failed")
	}
}

func (s *Server) handleRetryAttemptV1(w http.ResponseWriter, r *http.Request, id string, seq int, attemptID string) {
	if r.Method != http.MethodPost {
		writeV1Error(w, r, 405, "method_not_allowed", "Method not allowed")
		return
	}
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	existing, err := s.answerAttemptService.Get(r.Context(), identity.PrincipalID, id, attemptID)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	if existing.TurnSequence != seq {
		s.writeInterviewError(w, r, interview.ErrNotFound)
		return
	}
	attempt, err := s.answerAttemptService.Retry(r.Context(), identity.PrincipalID, id, attemptID)
	if err != nil {
		s.writeInterviewError(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"attempt": attempt})
}
