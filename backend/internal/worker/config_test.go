package worker

import "testing"

func TestConfigRejectsUnsafeJobRetention(t *testing.T) {
	t.Setenv("WORKER_JOB_RETENTION", "1h")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected short terminal job retention to be rejected")
	}
}

func TestConfigReadsGuestPreviewConcurrency(t *testing.T) {
	t.Setenv("WORKER_GUEST_PREVIEW_CONCURRENCY", "3")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("worker config: %v", err)
	}
	if config.GuestPreviewConcurrency != 3 {
		t.Fatalf("guest preview concurrency = %d", config.GuestPreviewConcurrency)
	}
}
