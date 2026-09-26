package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/auth"
)

func TestCORSAllowsConfiguredCredentialedOrigin(t *testing.T) {
	config, err := ParseCORSConfig(" https://app.example.com/ , http://localhost:3218 ")
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://app.example.com", "http://localhost:3218"} {
		t.Run(origin, func(t *testing.T) {
			called := false
			handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
			request := httptest.NewRequest(http.MethodOptions, "/api/user/data", nil)
			request.Header.Set("Origin", origin)
			request.Header.Set("Access-Control-Request-Method", http.MethodGet)
			request.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Not-Allowed")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || called {
				t.Fatalf("preflight must stop before handler: status %d, called %v", response.Code, called)
			}
			if response.Header().Get("Access-Control-Allow-Origin") != origin || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatalf("unexpected CORS headers: %v", response.Header())
			}
			if response.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, DELETE, OPTIONS" || response.Header().Get("Access-Control-Allow-Headers") != "Content-Type, X-Request-ID" {
				t.Fatalf("preflight grants undocumented methods or headers: %v", response.Header())
			}
			if response.Header().Get("Access-Control-Expose-Headers") != requestIDHeader {
				t.Fatalf("request id is not exposed to browser clients: %v", response.Header())
			}
			if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Origin") {
				t.Fatal("missing Vary: Origin")
			}
		})
	}
}

func TestCORSAllowsSwaggerMutationsFromConfiguredAPIOriginOnly(t *testing.T) {
	config, err := ParseCORSConfig("https://app.example.com,https://api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	for _, method := range []string{http.MethodOptions, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run("allowed "+method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/api/user/recordings", nil)
			request.Header.Set("Origin", "https://api.example.com")
			if method == http.MethodOptions {
				request.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			wantStatus := http.StatusCreated
			if method == http.MethodOptions {
				wantStatus = http.StatusNoContent
			}
			if response.Code != wantStatus || response.Header().Get("Access-Control-Allow-Origin") != "https://api.example.com" || response.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatalf("configured API docs origin failed: status=%d headers=%v", response.Code, response.Header())
			}
		})
	}

	request := httptest.NewRequest(http.MethodPost, "/api/user/recordings", nil)
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("untrusted docs origin received mutation access: status=%d headers=%v", response.Code, response.Header())
	}
}

func TestCORSRejectsUnsafeRequestFromUnknownOrigin(t *testing.T) {
	config, err := ParseCORSConfig("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://evil.example", "https://app.example.com.evil.example", "http://app.example.com", "https://app.example.com:444", "null"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
			t.Run(method+" "+origin, func(t *testing.T) {
				called := false
				handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
				request := httptest.NewRequest(method, "/api/auth/login", strings.NewReader(`{}`))
				request.Header.Set("Origin", origin)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusForbidden || called {
					t.Fatalf("expected rejection before handler, got %d, called %v", response.Code, called)
				}
				if response.Header().Get("Access-Control-Allow-Origin") != "" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
					t.Fatal("rejected origin received CORS permission")
				}
			})
		}
	}
}

func TestCORSActualRequestsAndNonBrowserClients(t *testing.T) {
	for _, tc := range []struct {
		name, allowlist, origin, method string
		status                          int
		allowed                         bool
	}{
		{"allowed read", "https://app.example.com", "https://app.example.com", "GET", 201, true},
		{"allowed write", "https://app.example.com", "https://app.example.com", "POST", 201, true},
		{"unknown read", "https://app.example.com", "https://other.example.com", "GET", 201, false},
		{"unknown head", "https://app.example.com", "https://other.example.com", "HEAD", 201, false},
		{"originless write", "https://app.example.com", "", "POST", 201, false},
		{"empty allowlist originless", "", "", "POST", 201, false},
		{"empty allowlist browser write", "", "https://app.example.com", "POST", 403, false},
		{"empty allowlist browser read", "", "https://app.example.com", "GET", 201, false},
		{"originless preflight", "", "", "OPTIONS", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := ParseCORSConfig(tc.allowlist)
			if err != nil {
				t.Fatal(err)
			}
			handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Add("Vary", "Accept-Encoding")
				w.WriteHeader(http.StatusCreated)
			}))
			request := httptest.NewRequest(tc.method, "/api/user/data", nil)
			request.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("got %d, want %d", response.Code, tc.status)
			}
			wantOrigin, wantCredentials := "", ""
			if tc.allowed {
				wantOrigin, wantCredentials = tc.origin, "true"
			}
			if response.Header().Get("Access-Control-Allow-Origin") != wantOrigin || response.Header().Get("Access-Control-Allow-Credentials") != wantCredentials {
				t.Fatalf("unexpected permission: %v", response.Header())
			}
			if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Origin") {
				t.Fatal("responses must vary by Origin, including denied origins")
			}
		})
	}
}

func TestCORSRejectsWildcardAndOriginPaths(t *testing.T) {
	for _, raw := range []string{"*", "https://*.example.com", "https://app.example.com/path", "https://app.example.com//", "https://app.example.com?query=1", "https://app.example.com?", "https://app.example.com#fragment", "https://app.example.com#", "https://user:password@app.example.com", "ftp://app.example.com", "app.example.com", "https://", "https:///path", "https://:443", "https://app.example.com:bad"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseCORSConfig(raw); err == nil {
				t.Fatalf("expected %q to fail", raw)
			}
		})
	}
}

func TestCORSIsAppliedToServerRoutes(t *testing.T) {
	config, err := ParseCORSConfig("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(Config{CORS: config}).Handler()
	for _, path := range []string{"/healthz", "/api/auth/session", "/uploads/example.webm", "/unknown"} {
		request := httptest.NewRequest(http.MethodOptions, path, nil)
		request.Header.Set("Origin", "https://app.example.com")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Fatalf("CORS missing on %s: %d %v", path, response.Code, response.Header())
		}
	}
}

func TestCookieServerClearsConfiguredScopeAndDefaultsToLax(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   auth.CookieConfig
		secure   bool
		sameSite http.SameSite
		domain   string
	}{
		{name: "zero config", sameSite: http.SameSiteLaxMode},
		{name: "cross site", config: auth.CookieConfig{Secure: true, SameSite: http.SameSiteNoneMode, Domain: ".example.com"}, secure: true, sameSite: http.SameSiteNoneMode, domain: "example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NODE_ENV", "production")
			handler := NewServer(Config{SessionCookie: tc.config}).Handler()
			for _, route := range []struct {
				method, path string
				status       int
			}{{"GET", "/api/auth/session", 401}, {"POST", "/api/auth/logout", 200}} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
				if response.Code != route.status {
					t.Fatalf("unexpected status: %d", response.Code)
				}
				cookies := response.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatalf("expected one clearing cookie, got %v", cookies)
				}
				cookie := cookies[0]
				if cookie.Name != "daily_speaking_session" || cookie.Path != "/" || cookie.Value != "" || cookie.MaxAge != -1 || !cookie.HttpOnly || cookie.Secure != tc.secure || cookie.SameSite != tc.sameSite || cookie.Domain != tc.domain {
					t.Fatalf("unexpected clearing cookie from %s: %#v", route.path, cookie)
				}
			}
		})
	}
}

func TestCookieServerPreservesSessionOnLookupFailure(t *testing.T) {
	// An unconfigured DB returns a real lookup error without starting a service.
	handler := NewServer(Config{
		SessionCookie: auth.CookieConfig{Secure: true, SameSite: http.SameSiteNoneMode, Domain: ".example.com"},
	}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "potentially-valid-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected lookup failure status 500, got %d", response.Code)
	}
	if cookies := response.Result().Header.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("lookup failure must preserve the existing session cookie, got Set-Cookie %v", cookies)
	}
}
