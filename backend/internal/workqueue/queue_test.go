package workqueue

import (
	"testing"
	"time"
)

func TestRetryDelayUsesCappedExponentialBackoff(t *testing.T) {
	base := 2 * time.Second
	maximum := 10 * time.Second
	for _, test := range []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 2 * time.Second},
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 4, want: 10 * time.Second},
	} {
		if got := RetryDelay(test.attempt, base, maximum); got != test.want {
			t.Fatalf("attempt %d: got %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestRunnerConfigDefaultsKeepHeartbeatInsideLease(t *testing.T) {
	config := (RunnerConfig{}).withDefaults()
	if config.Concurrency != 1 || config.PollInterval <= 0 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.HeartbeatInterval <= 0 || config.HeartbeatInterval >= config.LeaseDuration {
		t.Fatalf("heartbeat %s must be inside lease %s", config.HeartbeatInterval, config.LeaseDuration)
	}
}
