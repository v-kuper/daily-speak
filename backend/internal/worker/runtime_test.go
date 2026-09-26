package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/workqueue"
)

func TestRuntimeRejectsIncompleteProcessor(t *testing.T) {
	err := Run(context.Background(), workqueue.NewStore(nil), Config{}, Processor{})
	if err == nil {
		t.Fatal("expected incomplete worker processor to be rejected")
	}
}

func TestPoolConfigKeepsPerKindConcurrencyAndLeasePolicy(t *testing.T) {
	config := Config{
		PollInterval: time.Second, LeaseDuration: 2 * time.Minute,
		HeartbeatInterval: 30 * time.Second, RetryBaseDelay: 5 * time.Second,
		RetryMaxDelay: 5 * time.Minute,
	}
	pool := poolConfig(config, []string{workqueue.KindRecordingProcess}, 7, Processor{})
	if pool.Concurrency != 7 || len(pool.Kinds) != 1 || pool.Kinds[0] != workqueue.KindRecordingProcess {
		t.Fatalf("unexpected pool identity %#v", pool)
	}
	if pool.PollInterval != config.PollInterval || pool.LeaseDuration != config.LeaseDuration ||
		pool.HeartbeatInterval != config.HeartbeatInterval || pool.RetryBaseDelay != config.RetryBaseDelay ||
		pool.RetryMaxDelay != config.RetryMaxDelay {
		t.Fatalf("pool policy changed: %#v", pool)
	}
}

func TestMaintenanceRunsImmediatelyAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan time.Duration, 1)
	done := make(chan error, 1)
	go func() {
		done <- runMaintenanceLoop(ctx, Config{
			MediaSweepInterval: time.Hour,
			JobRetention:       14 * 24 * time.Hour,
		}, func(_ context.Context, retention time.Duration) {
			called <- retention
			cancel()
		})
	}()

	select {
	case retention := <-called:
		if retention != 14*24*time.Hour {
			t.Fatalf("retention = %s", retention)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance did not run immediately")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("maintenance error = %v", err)
	}
}
