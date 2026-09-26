package operations

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnvUsesSafeDefaults(t *testing.T) {
	for _, name := range []string{
		"TRUSTED_PROXY_CIDRS", "RATE_LIMIT_ENABLED", "RATE_LIMIT_AUTH_REQUESTS", "RATE_LIMIT_AUTH_WINDOW",
		"RATE_LIMIT_WRITE_REQUESTS", "RATE_LIMIT_WRITE_WINDOW", "RATE_LIMIT_EXPENSIVE_REQUESTS",
		"RATE_LIMIT_EXPENSIVE_WINDOW", "READINESS_TIMEOUT", "READINESS_MAX_QUEUE_DEPTH",
		"READINESS_MAX_OLDEST_JOB_AGE", "METRICS_BEARER_TOKEN",
	} {
		t.Setenv(name, "")
	}
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !config.RateLimitsEnabled || len(config.TrustedProxies) != 0 {
		t.Fatalf("unsafe defaults: %+v", config)
	}
	if config.AuthLimit.Requests != 20 || config.WriteLimit.Requests != 120 || config.ExpensiveLimit.Window != time.Hour {
		t.Fatalf("unexpected limits: %+v", config)
	}
	if config.MetricsBearerToken != "" {
		t.Fatal("metrics must stay disabled without an explicit secret")
	}
}

func TestConfigFromEnvRejectsInvalidOperationalValues(t *testing.T) {
	cases := []struct{ name, value string }{
		{"TRUSTED_PROXY_CIDRS", "10.0.0.1"},
		{"RATE_LIMIT_ENABLED", "sometimes"},
		{"RATE_LIMIT_AUTH_REQUESTS", "0"},
		{"RATE_LIMIT_WRITE_WINDOW", "forever"},
		{"READINESS_TIMEOUT", "0s"},
		{"READINESS_MAX_QUEUE_DEPTH", "-1"},
		{"METRICS_BEARER_TOKEN", "too-short"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			_, err := ConfigFromEnv()
			if err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
