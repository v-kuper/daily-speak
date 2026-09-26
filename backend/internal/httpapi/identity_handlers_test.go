package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMobileIdentityIsSafeWhenSigningSecretIsMissing(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/anonymous", strings.NewReader(`{"deviceName":"iPhone","platform":"ios"}`))
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var payload v1ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "identity_unavailable" || payload.Error.RequestID == "" || payload.Error.RequestID != response.Header().Get(requestIDHeader) {
		t.Fatalf("unexpected error: %+v", payload.Error)
	}
}

func TestMobileIdentityRejectsInvalidPayloadBeforeDatabase(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/api/v1/auth/register", body: `{"email":"bad","password":"123"}`},
		{path: "/api/v1/auth/login", body: `{"email":"bad","password":"123"}`},
		{path: "/api/v1/auth/refresh", body: `{}`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestMobileIdentityRejectsOversizedPayload(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	response := httptest.NewRecorder()
	body := `{"deviceName":"` + strings.Repeat("x", maxIdentityRequestBytes) + `"}`
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/anonymous", strings.NewReader(body)))
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"code":"payload_too_large"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
