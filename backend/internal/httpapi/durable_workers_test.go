package httpapi

import (
	"context"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/worker"
)

func startTestWorkers(t *testing.T, server *Server) {
	t.Helper()
	runtime := testBackgroundRuntime(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = runtime.Run(ctx, worker.Config{
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
