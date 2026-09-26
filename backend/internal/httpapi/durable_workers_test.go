package httpapi

import (
	"context"
	"testing"
	"time"
)

func startTestWorkers(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = server.RunWorkers(ctx, WorkerConfig{
			RecordingConcurrency: 1,
			ShadowingConcurrency: 1,
			CleanupConcurrency:   1,
			PollInterval:         5 * time.Millisecond,
			LeaseDuration:        2 * time.Second,
			HeartbeatInterval:    200 * time.Millisecond,
			RetryBaseDelay:       5 * time.Millisecond,
			RetryMaxDelay:        20 * time.Millisecond,
		})
	}()
}

func TestWorkerConfigRejectsUnsafeJobRetention(t *testing.T) {
	t.Setenv("WORKER_JOB_RETENTION", "1h")
	if _, err := WorkerConfigFromEnv(); err == nil {
		t.Fatal("expected short terminal job retention to be rejected")
	}
}
