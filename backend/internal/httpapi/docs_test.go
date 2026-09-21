package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIAndSwaggerAreServedByBackend(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	cases := []struct {
		path        string
		contentType string
		bodyPart    string
	}{
		{"/openapi.json", "application/json", `"openapi": "3.1.0"`},
		{"/docs", "text/html", "SwaggerUIBundle"},
	}

	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.path, recorder.Code)
		}
		if !strings.Contains(recorder.Header().Get("Content-Type"), tc.contentType) {
			t.Fatalf("%s: wrong content type %q", tc.path, recorder.Header().Get("Content-Type"))
		}
		if !strings.Contains(recorder.Body.String(), tc.bodyPart) {
			t.Fatalf("%s: missing %q", tc.path, tc.bodyPart)
		}
	}
}

func TestOpenAPIAndSwaggerRejectNonGETRequestsBeforeCORS(t *testing.T) {
	cors, err := ParseCORSConfig("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Config{CORS: cors}).Handler()
	cases := []struct {
		method string
		path   string
		origin string
	}{
		{http.MethodPost, "/openapi.json", "https://untrusted.example.com"},
		{http.MethodOptions, "/docs", "https://app.example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("Origin", tc.origin)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected 405, got %d with body %q", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("wrong content type %q", recorder.Header().Get("Content-Type"))
			}
			if strings.TrimSpace(recorder.Body.String()) != `{"error":"Method not allowed"}` {
				t.Fatalf("unexpected body %q", recorder.Body.String())
			}
		})
	}
}
