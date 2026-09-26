package httpapi

import (
	"context"
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/operations"
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
	checks := map[string]string{"database": "ok", "queue": "ok"}
	status := http.StatusOK
	if s.db == nil || s.db.Ping(ctx) != nil {
		checks["database"] = "unavailable"
		status = http.StatusServiceUnavailable
	} else {
		pressure, err := s.jobStore.Pressure(ctx)
		switch {
		case err != nil:
			checks["queue"] = "unavailable"
			status = http.StatusServiceUnavailable
		case s.operations.ReadyMaxQueueDepth > 0 && pressure.ActiveCount > int64(s.operations.ReadyMaxQueueDepth):
			checks["queue"] = "depth_exceeded"
			status = http.StatusServiceUnavailable
		case s.operations.ReadyMaxOldestJob > 0 && pressure.OldestAgeSeconds > s.operations.ReadyMaxOldestJob.Seconds():
			checks["queue"] = "oldest_job_exceeded"
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, map[string]any{"ok": status == http.StatusOK, "checks": checks})
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
	if s.db == nil || s.jobStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Metrics unavailable"})
		return
	}
	stats, err := s.jobStore.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Metrics unavailable"})
		return
	}
	pool := s.db.PoolStats()
	queues := make([]operations.QueueSnapshot, 0, len(stats))
	for _, stat := range stats {
		queues = append(queues, operations.QueueSnapshot{
			Kind: stat.Kind, State: stat.State, Count: stat.Count,
			OldestAgeSeconds: stat.OldestAgeSeconds, RecentTerminal: stat.RecentTerminal,
		})
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	s.metrics.WritePrometheus(w, operations.DatabaseSnapshot{
		TotalConns: pool.TotalConns, IdleConns: pool.IdleConns,
		AcquiredConns: pool.AcquiredConns, MaxConns: pool.MaxConns,
	}, queues)
}
