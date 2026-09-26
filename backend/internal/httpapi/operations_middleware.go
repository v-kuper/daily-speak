package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/operations"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (s *Server) withRequestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		finish := s.metrics.Begin()
		response := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		route := operationalRoute(r.URL.Path)
		finish(operationalMethod(r.Method), route, status, duration)
		logging.ForRequest("api.http", r).Info("request.completed", map[string]any{
			"status": status, "durationMs": logging.ElapsedMs(started),
			"route": route, "traceId": traceIDFrom(r),
		})
	})
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		if r.URL.Path == "/docs" {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		} else {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		if s.network.IsHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

type ratePolicy struct {
	scope     string
	limit     operations.Limit
	principal bool
	alsoIP    bool
}

func (s *Server) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.operations.RateLimitsEnabled || s.limiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		policy, ok := s.ratePolicy(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		ip := s.network.ClientIP(r).String()
		if ip == "invalid IP" || ip == "" {
			ip = "unknown"
		}
		subjects := []string{"ip:" + ip}
		if policy.principal {
			if principal := s.rateLimitPrincipal(r); principal != "" {
				subjects = []string{"principal:" + principal}
				if policy.alsoIP {
					subjects = append(subjects, "ip:"+ip)
				}
			}
		}
		var tightest operations.Decision
		for _, subject := range subjects {
			decision, err := s.limiter.Allow(r.Context(), policy.scope, subject, policy.limit)
			if err != nil {
				logging.ForRequest("api.rate_limit", r).Error("rate_limit.failed", logging.ErrorMeta(err))
				s.writeOperationalError(w, r, http.StatusServiceUnavailable, "service_unavailable", "Service is temporarily unavailable")
				return
			}
			if tightest.Limit == 0 || decision.Remaining < tightest.Remaining {
				tightest = decision
			}
			if !decision.Allowed {
				s.writeRateLimitHeaders(w, decision)
				logging.ForRequest("api.rate_limit", r).Warn("request.rejected", map[string]any{
					"status": http.StatusTooManyRequests, "scope": policy.scope,
				})
				s.writeOperationalError(w, r, http.StatusTooManyRequests, "rate_limited", "Too many requests")
				return
			}
		}
		s.writeRateLimitHeaders(w, tightest)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ratePolicy(r *http.Request) (ratePolicy, bool) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if r.Method == http.MethodOptions || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return ratePolicy{}, false
	}
	switch path {
	case "/api/v1/auth/anonymous", "/api/v1/auth/register", "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/auth/register", "/api/auth/login":
		return ratePolicy{scope: "auth", limit: s.operations.AuthLimit}, true
	case "/api/v1/guest/previews":
		return ratePolicy{scope: "guest_preview", limit: s.operations.ExpensiveLimit, principal: true, alsoIP: true}, true
	case "/api/v1/recordings", "/api/user/recordings":
		return ratePolicy{scope: "recording_create", limit: s.operations.ExpensiveLimit, principal: true, alsoIP: true}, true
	}
	if strings.HasPrefix(path, "/api/") {
		return ratePolicy{scope: "api_write", limit: s.operations.WriteLimit, principal: true}, true
	}
	return ratePolicy{}, false
}

func (s *Server) rateLimitPrincipal(r *http.Request) string {
	token, supplied := bearerToken(r)
	if !supplied || token == "" {
		return ""
	}
	claims, err := auth.ParseAccessToken(s.identityTokens, token)
	if err != nil {
		return ""
	}
	return claims.PrincipalID
}

func (s *Server) writeRateLimitHeaders(w http.ResponseWriter, decision operations.Decision) {
	if decision.Limit == 0 {
		return
	}
	w.Header().Set("RateLimit-Limit", strconv.Itoa(decision.Limit))
	w.Header().Set("RateLimit-Remaining", strconv.Itoa(decision.Remaining))
	resetSeconds := int64((decision.ResetAfter + time.Second - 1) / time.Second)
	w.Header().Set("RateLimit-Reset", strconv.FormatInt(resetSeconds, 10))
	if decision.RetryAfter > 0 {
		seconds := int64((decision.RetryAfter + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	}
}

func (s *Server) writeOperationalError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	if strings.HasPrefix(r.URL.Path, "/api/v1") {
		writeV1Error(w, r, status, code, message)
		return
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func operationalRoute(path string) string {
	path = strings.TrimSuffix(path, "/")
	switch {
	case path == "":
		return "/"
	case path == "/healthz" || path == "/readyz" || path == "/metrics" || path == "/docs" || path == "/openapi.json":
		return path
	case isOperationalStaticRoute(path):
		return path
	case strings.HasPrefix(path, "/api/v1/auth/sessions/"):
		return "/api/v1/auth/sessions/{id}"
	case strings.HasPrefix(path, "/api/v1/media/uploads/"):
		return "/api/v1/media/uploads/{id}"
	case strings.HasPrefix(path, "/api/v1/media/"):
		return "/api/v1/media/{id}/download"
	case strings.HasPrefix(path, "/api/v1/guest/previews/"):
		return "/api/v1/guest/previews/{id}"
	case strings.HasPrefix(path, "/api/v1/recordings/"):
		return "/api/v1/recordings/{id}"
	case strings.HasPrefix(path, "/api/recordings/"):
		return "/api/recordings/{id}"
	case strings.HasPrefix(path, "/api/recording-sessions/"):
		return "/api/recording-sessions/{id}"
	case strings.HasPrefix(path, "/api/feed/posts/"):
		return "/api/feed/posts/{id}"
	case strings.HasPrefix(path, "/api/feed/replies/"):
		return "/api/feed/replies/{id}"
	case strings.HasPrefix(path, "/uploads/"):
		return "/uploads/{asset}"
	case strings.HasPrefix(path, "/api/"):
		return "api_unmatched"
	default:
		return "unmatched"
	}
}

var operationalStaticRoutes = map[string]struct{}{
	"/api/v1":                {},
	"/api/v1/auth/anonymous": {}, "/api/v1/auth/register": {}, "/api/v1/auth/login": {},
	"/api/v1/auth/refresh": {}, "/api/v1/auth/session": {}, "/api/v1/auth/logout": {},
	"/api/v1/auth/logout-all": {}, "/api/v1/auth/sessions": {},
	"/api/v1/guest/previews": {}, "/api/v1/recordings": {}, "/api/v1/media/uploads": {},
	"/api/auth/register": {}, "/api/auth/login": {}, "/api/auth/session": {}, "/api/auth/logout": {},
	"/api/daily-questions": {}, "/api/topic-guidance": {}, "/api/study-words": {},
	"/api/user/data": {}, "/api/user/interests": {}, "/api/user/ollama-model": {},
	"/api/user/subscription": {}, "/api/user/english-level": {}, "/api/user/recordings": {},
	"/api/recording-sessions": {}, "/api/feed/posts": {},
}

func isOperationalStaticRoute(path string) bool {
	_, found := operationalStaticRoutes[path]
	return found
}

func operationalMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func bearerMatches(header string, expected string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	provided := []byte(parts[1])
	wanted := []byte(expected)
	return len(provided) == len(wanted) && subtle.ConstantTimeCompare(provided, wanted) == 1
}
