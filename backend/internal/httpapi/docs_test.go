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
