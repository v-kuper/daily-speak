package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if body := strings.TrimSpace(recorder.Body.String()); body != `{"ok":true}` {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestEveryResponseHasRequestID(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	cases := []struct {
		name       string
		requestID  string
		wantSameID bool
	}{
		{name: "server generated"},
		{name: "safe caller id", requestID: "ios-01J8Z.TEST:42", wantSameID: true},
		{name: "unsafe caller id", requestID: "bad id\nvalue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			request.Header.Set("X-Request-ID", tc.requestID)
			handler.ServeHTTP(recorder, request)
			got := recorder.Header().Get("X-Request-ID")
			if got == "" || strings.ContainsAny(got, " \r\n") {
				t.Fatalf("invalid response request id %q", got)
			}
			if tc.wantSameID && got != tc.requestID {
				t.Fatalf("request id = %q, want %q", got, tc.requestID)
			}
			if !tc.wantSameID && tc.requestID != "" && got == tc.requestID {
				t.Fatalf("unsafe request id was retained: %q", got)
			}
		})
	}
}

func TestV1MetadataAndStableErrors(t *testing.T) {
	handler := NewServer(Config{}).Handler()

	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/api/v1", nil))
	if metadata.Code != http.StatusOK || strings.TrimSpace(metadata.Body.String()) != `{"version":"v1","status":"stable","documentation":"/docs"}` {
		t.Fatalf("unexpected metadata response %d: %s", metadata.Code, metadata.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/recordings/demo", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", unauthorized.Code, unauthorized.Body.String())
	}
	var payload struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(unauthorized.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode v1 error: %v", err)
	}
	if payload.Error.Code != "unauthorized" || payload.Error.Message != "Unauthorized" || payload.Error.RequestID == "" {
		t.Fatalf("unexpected v1 error: %+v", payload.Error)
	}
	if payload.Error.RequestID != unauthorized.Header().Get("X-Request-ID") {
		t.Fatalf("body and header request IDs differ")
	}
}

func TestLegacyErrorsRemainCompatible(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/recordings/demo", nil))
	if strings.TrimSpace(recorder.Body.String()) != `{"error":"Unauthorized"}` {
		t.Fatalf("legacy error contract changed: %s", recorder.Body.String())
	}
}

func TestV1ErrorCodesAreStableByStatus(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:            "invalid_request",
		http.StatusUnauthorized:          "unauthorized",
		http.StatusPaymentRequired:       "payment_required",
		http.StatusForbidden:             "forbidden",
		http.StatusNotFound:              "not_found",
		http.StatusMethodNotAllowed:      "method_not_allowed",
		http.StatusConflict:              "conflict",
		http.StatusRequestEntityTooLarge: "payload_too_large",
		http.StatusTooManyRequests:       "rate_limited",
		http.StatusInternalServerError:   "internal_error",
		http.StatusBadGateway:            "upstream_unavailable",
		http.StatusServiceUnavailable:    "service_unavailable",
	}
	for status, want := range cases {
		if got := v1ErrorCode(status); got != want {
			t.Fatalf("status %d: code %q, want %q", status, got, want)
		}
	}
}

func TestBackendDoesNotServeOrProxyWebRoutes(t *testing.T) {
	handler := NewServer(Config{}).Handler()

	for _, path := range []string{"/", "/speak", "/history/demo"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", path, recorder.Code)
		}
		if strings.TrimSpace(recorder.Body.String()) != `{"error":"Not found"}` {
			t.Fatalf("%s: unexpected body %q", path, recorder.Body.String())
		}
	}
}

func TestUnauthorizedAPIContractWithoutCookie(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/auth/session", ""},
		{http.MethodGet, "/api/user/data", ""},
		{http.MethodPost, "/api/user/recordings", `{"recording":{}}`},
		{http.MethodGet, "/api/recordings/demo-recording", ""},
		{http.MethodDelete, "/api/recordings/demo-recording", ""},
		{http.MethodPost, "/api/recordings/demo-recording/retry", ""},
		{http.MethodPost, "/api/recordings/demo-recording/shadowing", ""},
		{http.MethodPost, "/api/recording-sessions", `{"topic":"Free talk"}`},
		{http.MethodPost, "/api/recording-sessions/demo-session/chunks", ""},
		{http.MethodPost, "/api/recording-sessions/demo-session/audio", ""},
		{http.MethodPost, "/api/recording-sessions/demo-session/finish", "{}"},
		{http.MethodGet, "/api/feed/posts", ""},
		{http.MethodPost, "/api/feed/posts", `{"recordingId":"demo"}`},
		{http.MethodGet, "/api/feed/posts/demo-post", ""},
		{http.MethodPost, "/api/feed/posts/demo-post/replies", `{"duration":10,"audioDataUrl":"data:audio/webm;base64,AAAA"}`},
		{http.MethodPost, "/api/feed/posts/demo-post/reactions", `{"reaction":"like"}`},
		{http.MethodPost, "/api/feed/replies/demo-reply/reactions", `{"reaction":"like"}`},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d with body %s", recorder.Code, recorder.Body.String())
			}
			if strings.TrimSpace(recorder.Body.String()) != `{"error":"Unauthorized"}` {
				t.Fatalf("unexpected body %q", recorder.Body.String())
			}
		})
	}
}

func TestAuthValidationContract(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	cases := []struct {
		path string
		body string
	}{
		{"/api/auth/register", `{"email":"bad-email","password":"123"}`},
		{"/api/auth/login", `{"email":"bad-email","password":"123"}`},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d with body %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "Enter a valid email address.") {
				t.Fatalf("unexpected body %q", recorder.Body.String())
			}
		})
	}
}
