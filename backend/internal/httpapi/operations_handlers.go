package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	timeout := s.operations.ReadinessTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	readiness := s.operationsMonitor.CheckReadiness(ctx, s.operations.ReadyMaxQueueDepth, s.operations.ReadyMaxOldestJob)
	status := http.StatusOK
	if !readiness.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ok": readiness.Ready, "checks": readiness.Checks})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	if s.operations.MetricsBearerToken == "" || !bearerMatches(r.Header.Get("Authorization"), s.operations.MetricsBearerToken) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	snapshot, err := s.operationsMonitor.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Metrics unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	s.metrics.WritePrometheus(w, snapshot.Database, snapshot.Queues)
}
