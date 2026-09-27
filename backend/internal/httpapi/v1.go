package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

type v1ErrorEnvelope struct {
	Error v1Error `json:"error"`
}

type v1Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func writeV1Error(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	writeJSON(w, status, v1ErrorEnvelope{Error: v1Error{Code: code, Message: message, RequestID: requestIDFrom(r)}})
}

func (s *Server) routeV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	buffered := newBufferedResponse()
	s.dispatchV1(buffered, r)
	commitV1Response(w, r, buffered)
}

func (s *Server) dispatchV1(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if s.routeGuestPreviewV1(w, r, path) {
		return
	}
	if s.routeMediaV1(w, r, path) {
		return
	}
	switch {
	case path == "/api/v1/auth/anonymous" && r.Method == http.MethodPost:
		s.handleAnonymousIdentityV1(w, r)
	case path == "/api/v1/auth/register" && r.Method == http.MethodPost:
		s.handleRegisterIdentityV1(w, r)
	case path == "/api/v1/auth/login" && r.Method == http.MethodPost:
		s.handleLoginIdentityV1(w, r)
	case path == "/api/v1/auth/refresh" && r.Method == http.MethodPost:
		s.handleRefreshIdentityV1(w, r)
	case path == "/api/v1/auth/session" && r.Method == http.MethodGet:
		s.handleIdentitySessionV1(w, r)
	case path == "/api/v1/auth/logout" && r.Method == http.MethodPost:
		s.handleLogoutIdentityV1(w, r)
	case path == "/api/v1/auth/logout-all" && r.Method == http.MethodPost:
		s.handleLogoutAllIdentityV1(w, r)
	case path == "/api/v1/auth/sessions" && r.Method == http.MethodGet:
		s.handleListIdentitySessionsV1(w, r)
	case strings.HasPrefix(path, "/api/v1/auth/sessions/") && !strings.Contains(strings.TrimPrefix(path, "/api/v1/auth/sessions/"), "/") && r.Method == http.MethodDelete:
		s.handleRevokeIdentitySessionV1(w, r, strings.TrimPrefix(path, "/api/v1/auth/sessions/"))
	case isIdentityV1Resource(path):
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	case path == "/api/v1" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, struct {
			Version       string `json:"version"`
			Status        string `json:"status"`
			Documentation string `json:"documentation"`
		}{Version: "v1", Status: "stable", Documentation: "/docs"})
	case path == "/api/v1":
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	case path == "/api/v1/recordings" && r.Method == http.MethodGet:
		s.handleListRecordingsV1(w, r)
	case path == "/api/v1/recordings" && r.Method == http.MethodPost:
		s.handleCreateRecordingV1(w, r)
	case path == "/api/v1/recordings":
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	case strings.HasPrefix(path, "/api/v1/recordings/"):
		s.routeRecordingV1(w, r, strings.TrimPrefix(path, "/api/v1/recordings/"))
	case path == "/api/v1/practice/daily-questions" && r.Method == http.MethodGet:
		s.handleDailyQuestions(w, r)
	case path == "/api/v1/practice/topic-guidance" && r.Method == http.MethodGet:
		s.handleTopicGuidance(w, r)
	case path == "/api/v1/practice/study-words" && r.Method == http.MethodGet:
		s.handleStudyWords(w, r)
	case path == "/api/v1/profile" && r.Method == http.MethodGet:
		s.handleUserData(w, r)
	case path == "/api/v1/profile/interests" && r.Method == http.MethodPut:
		s.handleUserInterests(w, r)
	case path == "/api/v1/profile/english-level" && r.Method == http.MethodGet:
		s.handleGetEnglishLevel(w, r)
	case path == "/api/v1/profile/english-level" && r.Method == http.MethodPut:
		s.handlePutEnglishLevel(w, r)
	case path == "/api/v1/subscription" && r.Method == http.MethodGet:
		s.handleGetSubscription(w, r)
	case path == "/api/v1/subscription" && r.Method == http.MethodPost:
		s.handleActivateSubscription(w, r)
	case path == "/api/v1/subscription" && r.Method == http.MethodDelete:
		s.handleCancelSubscription(w, r)
	case isApplicationV1Resource(path):
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	default:
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Not found")
	}
}

func isApplicationV1Resource(path string) bool {
	switch path {
	case "/api/v1/practice/daily-questions", "/api/v1/practice/topic-guidance", "/api/v1/practice/study-words",
		"/api/v1/profile", "/api/v1/profile/interests", "/api/v1/profile/english-level", "/api/v1/subscription":
		return true
	default:
		return false
	}
}

func isIdentityV1Resource(path string) bool {
	switch path {
	case "/api/v1/auth/anonymous", "/api/v1/auth/register", "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/session", "/api/v1/auth/logout", "/api/v1/auth/logout-all", "/api/v1/auth/sessions":
		return true
	}
	return strings.HasPrefix(path, "/api/v1/auth/sessions/") && !strings.Contains(strings.TrimPrefix(path, "/api/v1/auth/sessions/"), "/")
}

func commitV1Response(w http.ResponseWriter, r *http.Request, buffered *bufferedResponse) {
	for name, values := range buffered.header {
		if strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	status := buffered.status
	if status == 0 {
		status = http.StatusOK
	}
	if status < http.StatusBadRequest {
		w.WriteHeader(status)
		_, _ = w.Write(buffered.body.Bytes())
		return
	}
	var structured v1ErrorEnvelope
	if json.Unmarshal(buffered.body.Bytes(), &structured) == nil && strings.TrimSpace(structured.Error.Code) != "" {
		structured.Error.RequestID = requestIDFrom(r)
		writeJSON(w, status, structured)
		return
	}
	writeJSON(w, status, v1ErrorEnvelope{Error: v1Error{
		Code:      v1ErrorCode(status),
		Message:   http.StatusText(status),
		RequestID: requestIDFrom(r),
	}})
}

func v1ErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusPaymentRequired:
		return "payment_required"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusBadGateway:
		return "upstream_unavailable"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_error"
		}
		return "request_failed"
	}
}
