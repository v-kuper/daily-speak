package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/operations"
)

type fakeRequestLimiter struct {
	decision operations.Decision
	err      error
	scope    string
	subject  string
}

func (f *fakeRequestLimiter) Allow(_ context.Context, scope string, subject string, _ operations.Limit) (operations.Decision, error) {
	f.scope, f.subject = scope, subject
	return f.decision, f.err
}

func TestLivenessDoesNotDependOnReadiness(t *testing.T) {
	handler := newTestServer(Config{Operations: operations.Config{ReadinessTimeout: time.Second}}).Handler()
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("liveness = %d", health.Code)
	}
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable || !strings.Contains(ready.Body.String(), `"database":"unavailable"`) {
		t.Fatalf("readiness = %d %s", ready.Code, ready.Body.String())
	}
}

func TestTraceContextAndSecurityHeaders(t *testing.T) {
	config := operations.Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}}
	handler := newTestServer(Config{Operations: config}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.RemoteAddr = "172.18.0.3:1000"
	request.Header.Set("X-Forwarded-Proto", "https")
	parent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	request.Header.Set("Traceparent", parent)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Traceparent"); !strings.HasPrefix(got, "00-4bf92f3577b34da6a3ce929d0e0e4736-") || got == parent {
		t.Fatalf("traceparent = %q", got)
	}
	for name, expected := range map[string]string{
		"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
		"Referrer-Policy": "no-referrer", "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	} {
		if response.Header().Get(name) != expected {
			t.Fatalf("%s = %q", name, response.Header().Get(name))
		}
	}
}

func TestInvalidTraceparentIsReplacedAndMetricsAreHiddenByDefault(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	healthRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRequest.Header.Set("Traceparent", "00-not-valid")
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, healthRequest)
	if got := health.Header().Get("Traceparent"); len(got) != 55 || got == "00-not-valid" {
		t.Fatalf("traceparent = %q", got)
	}
	metrics := httptest.NewRecorder()
	handler.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusNotFound {
		t.Fatalf("metrics without secret = %d", metrics.Code)
	}
}

func TestRateLimitReturnsStableV1ErrorAndRetryMetadata(t *testing.T) {
	server := newTestServer(Config{Operations: operations.Config{
		RateLimitsEnabled: true,
		AuthLimit:         operations.Limit{Requests: 2, Window: time.Minute},
	}})
	limiter := &fakeRequestLimiter{decision: operations.Decision{
		Allowed: false, Limit: 2, Remaining: 0,
		ResetAfter: 42 * time.Second, RetryAfter: 42 * time.Second,
	}}
	server.limiter = limiter
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	request.RemoteAddr = "198.51.100.9:4321"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Retry-After") != "42" || response.Header().Get("RateLimit-Reset") != "42" {
		t.Fatalf("rate metadata = %v", response.Header())
	}
	if limiter.scope != "auth" || limiter.subject != "ip:198.51.100.9" {
		t.Fatalf("limiter key = %s %s", limiter.scope, limiter.subject)
	}
}

func TestRateLimitFailsClosedWhenSharedStoreIsUnavailable(t *testing.T) {
	server := newTestServer(Config{Operations: operations.Config{
		RateLimitsEnabled: true,
		WriteLimit:        operations.Limit{Requests: 10, Window: time.Minute},
	}})
	server.limiter = &fakeRequestLimiter{err: errors.New("database unavailable")}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/v1/auth/logout", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"service_unavailable"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestOperationalRoutesDoNotUseResourceIdentifiers(t *testing.T) {
	cases := map[string]string{
		"/api/v1/recordings/secret-recording-id":       "/api/v1/recordings/{id}",
		"/api/v1/media/uploads/private-upload/parts/1": "/api/v1/media/uploads/{id}",
		"/api/feed/posts/private-post/replies":         "/api/feed/posts/{id}",
		"/api/private-value/another-secret":            "api_unmatched",
		"/unexpected/private-value":                    "unmatched",
	}
	for path, expected := range cases {
		if got := operationalRoute(path); got != expected {
			t.Fatalf("%s = %s, want %s", path, got, expected)
		}
	}
	if got := operationalMethod("PRIVATE-METHOD"); got != "OTHER" {
		t.Fatalf("unknown method = %s", got)
	}
}
