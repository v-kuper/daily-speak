package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
)

const traceparentHeader = "Traceparent"

var validTraceparent = regexp.MustCompile(`^00-([a-f0-9]{32})-([a-f0-9]{16})-([a-f0-9]{2})$`)

type traceContextKey struct{}

type traceContext struct {
	TraceID string
	SpanID  string
	Flags   string
}

func withTraceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := newTraceContext(r.Header.Get(traceparentHeader))
		value := "00-" + trace.TraceID + "-" + trace.SpanID + "-" + trace.Flags
		r.Header.Set(traceparentHeader, value)
		w.Header().Set(traceparentHeader, value)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceContextKey{}, trace)))
	})
}

func newTraceContext(parent string) traceContext {
	flags := "01"
	traceID := ""
	match := validTraceparent.FindStringSubmatch(strings.ToLower(strings.TrimSpace(parent)))
	if len(match) == 4 && match[1] != strings.Repeat("0", 32) && match[2] != strings.Repeat("0", 16) {
		traceID = match[1]
		flags = match[3]
	}
	if traceID == "" {
		traceID = randomHex(16)
	}
	return traceContext{TraceID: traceID, SpanID: randomHex(8), Flags: flags}
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic("cryptographic randomness unavailable: " + err.Error())
	}
	return hex.EncodeToString(value)
}

func traceIDFrom(r *http.Request) string {
	trace, _ := r.Context().Value(traceContextKey{}).(traceContext)
	return trace.TraceID
}
