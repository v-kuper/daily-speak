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

func (s *Server) routeV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	buffered := newBufferedResponse()
	s.dispatchV1(buffered, r)
	commitV1Response(w, r, buffered)
}

func (s *Server) dispatchV1(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/v1" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, struct {
			Version       string `json:"version"`
			Status        string `json:"status"`
			Documentation string `json:"documentation"`
		}{Version: "v1", Status: "stable", Documentation: "/docs"})
	case path == "/api/v1":
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	case path == "/api/v1/recordings" && r.Method == http.MethodGet:
		s.handleListRecordingsV1(w, r)
	case path == "/api/v1/recordings":
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	case strings.HasPrefix(path, "/api/v1/recordings/") && !strings.Contains(strings.TrimPrefix(path, "/api/v1/recordings/"), "/") && r.Method == http.MethodGet:
		s.handleGetRecording(w, r, strings.TrimPrefix(path, "/api/v1/recordings/"))
	case strings.HasPrefix(path, "/api/v1/recordings/") && !strings.Contains(strings.TrimPrefix(path, "/api/v1/recordings/"), "/"):
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
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
	message := http.StatusText(status)
	var legacy struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(buffered.body.Bytes(), &legacy) == nil && strings.TrimSpace(legacy.Error) != "" {
		message = legacy.Error
	}
	writeJSON(w, status, v1ErrorEnvelope{Error: v1Error{
		Code:      v1ErrorCode(status),
		Message:   message,
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
