package httpapi

import (
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
