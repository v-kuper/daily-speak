package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
)

func TestMobileIdentityIsSafeWhenSigningSecretIsMissing(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
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
	handler := newTestServer(Config{}).Handler()
	for _, tc := range []struct {
		path string
		body string
	}{
		{path: "/api/v1/auth/register", body: `{"email":"bad","password":"123"}`},
		{path: "/api/v1/auth/login", body: `{"email":"bad","password":"123"}`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_request"`) {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestBrowserRefreshWithoutCookieIsUnauthorized(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{}`))
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"invalid_refresh_token"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestBrowserIdentityGrantKeepsRefreshTokenOutOfJSON(t *testing.T) {
	now := time.Now().UTC()
	grant := auth.TokenGrant{
		Identity: auth.Identity{PrincipalID: "user-id", Kind: "user", User: &auth.User{
			Email: "person@example.test", EnglishLevel: "B1",
		}},
		Session:     auth.DeviceSession{ID: "session-id", Platform: "web", ExpiresAt: now.Add(time.Hour), LastSeenAt: now, CreatedAt: now},
		AccessToken: "access-token", AccessTokenExpiresAt: now.Add(15 * time.Minute),
		RefreshToken: "refresh-token", RefreshTokenExpiresAt: now.Add(time.Hour),
	}
	server := &Server{browserCookie: auth.CookieConfig{Secure: true, SameSite: http.SameSiteLaxMode}}
	response := httptest.NewRecorder()
	server.writeIdentityGrant(response, http.StatusOK, grant, true)
	if strings.Contains(response.Body.String(), "refresh-token") || strings.Contains(response.Body.String(), "refreshTokenExpiresAt") {
		t.Fatalf("browser response exposed refresh credential: %s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.RefreshCookieName || cookies[0].Value != grant.RefreshToken || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("unexpected browser refresh cookie: %#v", cookies)
	}
}

func TestBrowserIdentityTransportCannotBeDowngradedByPlatform(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.Header.Set("Origin", "https://web.example.test")
	if !isBrowserIdentityRequest(request, "ios") {
		t.Fatal("a browser Origin must force the HttpOnly refresh transport")
	}
	native := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	if isBrowserIdentityRequest(native, "ios") {
		t.Fatal("an originless native request must receive the native token response")
	}
	if !isBrowserIdentityRequest(native, "web") {
		t.Fatal("the explicit web platform must select the browser transport")
	}
}

func TestMobileIdentityRejectsOversizedPayload(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	response := httptest.NewRecorder()
	body := `{"deviceName":"` + strings.Repeat("x", maxIdentityRequestBytes) + `"}`
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/anonymous", strings.NewReader(body)))
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), `"code":"payload_too_large"`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}
