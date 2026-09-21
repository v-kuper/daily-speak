package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"daily-speaking-practice/backend/internal/logging"
)

// CORSConfig holds an exact-origin allowlist that is immutable after parsing.
// Its zero value permits originless clients without granting browser access.
type CORSConfig struct {
	allowedOrigins map[string]struct{}
}

func ParseCORSConfig(raw string) (CORSConfig, error) {
	config := CORSConfig{allowedOrigins: make(map[string]struct{})}
	for _, entry := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(entry)
		if origin == "" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || strings.ContainsAny(origin, "*?#") {
			return CORSConfig{}, fmt.Errorf("CORS_ALLOWED_ORIGINS must contain only exact http or https origins without credentials, paths, queries, fragments, or wildcards")
		}
		config.allowedOrigins[strings.TrimSuffix(origin, "/")] = struct{}{}
	}
	return config, nil
}

func (c CORSConfig) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every response varies, including denials, so caches cannot reuse an
		// originless or denied response for an allowed browser origin.
		w.Header().Add("Vary", "Origin")
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		_, allowed := c.allowedOrigins[origin]
		if !allowed && ((origin != "" && r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Method == http.MethodOptions) {
			logging.ForRequest("api.cors", r).Warn("request.rejected", map[string]any{
				"status": http.StatusForbidden,
				"origin": origin,
				"reason": "origin_not_allowed",
			})
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Origin not allowed"})
			return
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
