package operations

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Limit struct {
	Requests int
	Window   time.Duration
}

type Config struct {
	TrustedProxies     []netip.Prefix
	RateLimitsEnabled  bool
	AuthLimit          Limit
	WriteLimit         Limit
	ExpensiveLimit     Limit
	ReadinessTimeout   time.Duration
	ReadyMaxQueueDepth int
	ReadyMaxOldestJob  time.Duration
	MetricsBearerToken string
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		RateLimitsEnabled:  true,
		AuthLimit:          Limit{Requests: 20, Window: time.Minute},
		WriteLimit:         Limit{Requests: 120, Window: time.Minute},
		ExpensiveLimit:     Limit{Requests: 20, Window: time.Hour},
		ReadinessTimeout:   2 * time.Second,
		ReadyMaxQueueDepth: 1000,
		ReadyMaxOldestJob:  30 * time.Minute,
		MetricsBearerToken: strings.TrimSpace(os.Getenv("METRICS_BEARER_TOKEN")),
	}
	var err error
	if config.TrustedProxies, err = parsePrefixes(os.Getenv("TRUSTED_PROXY_CIDRS")); err != nil {
		return Config{}, err
	}
	if config.RateLimitsEnabled, err = optionalBool("RATE_LIMIT_ENABLED", config.RateLimitsEnabled); err != nil {
		return Config{}, err
	}
	if config.AuthLimit.Requests, err = positiveInt("RATE_LIMIT_AUTH_REQUESTS", config.AuthLimit.Requests, 1_000_000); err != nil {
		return Config{}, err
	}
	if config.AuthLimit.Window, err = positiveDuration("RATE_LIMIT_AUTH_WINDOW", config.AuthLimit.Window); err != nil {
		return Config{}, err
	}
	if config.WriteLimit.Requests, err = positiveInt("RATE_LIMIT_WRITE_REQUESTS", config.WriteLimit.Requests, 1_000_000); err != nil {
		return Config{}, err
	}
	if config.WriteLimit.Window, err = positiveDuration("RATE_LIMIT_WRITE_WINDOW", config.WriteLimit.Window); err != nil {
		return Config{}, err
	}
	if config.ExpensiveLimit.Requests, err = positiveInt("RATE_LIMIT_EXPENSIVE_REQUESTS", config.ExpensiveLimit.Requests, 1_000_000); err != nil {
		return Config{}, err
	}
	if config.ExpensiveLimit.Window, err = positiveDuration("RATE_LIMIT_EXPENSIVE_WINDOW", config.ExpensiveLimit.Window); err != nil {
		return Config{}, err
	}
	if config.ReadinessTimeout, err = positiveDuration("READINESS_TIMEOUT", config.ReadinessTimeout); err != nil {
		return Config{}, err
	}
	if config.ReadyMaxQueueDepth, err = positiveInt("READINESS_MAX_QUEUE_DEPTH", config.ReadyMaxQueueDepth, 10_000_000); err != nil {
		return Config{}, err
	}
	if config.ReadyMaxOldestJob, err = positiveDuration("READINESS_MAX_OLDEST_JOB_AGE", config.ReadyMaxOldestJob); err != nil {
		return Config{}, err
	}
	if config.MetricsBearerToken != "" && len(config.MetricsBearerToken) < 32 {
		return Config{}, errors.New("METRICS_BEARER_TOKEN must contain at least 32 characters when configured")
	}
	return config, nil
}

func parsePrefixes(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR %q", item)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func optionalBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean value", name)
	}
	return value, nil
}

func positiveInt(name string, fallback int, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between 1 and %d", name, maximum)
	}
	return value, nil
}

func positiveDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	if value > 30*24*time.Hour {
		return 0, errors.New(name + " must not exceed 720h")
	}
	return value, nil
}
