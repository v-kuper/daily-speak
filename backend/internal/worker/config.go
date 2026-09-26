package worker

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RecordingConcurrency    int
	GuestPreviewConcurrency int
	ShadowingConcurrency    int
	CleanupConcurrency      int
	PollInterval            time.Duration
	LeaseDuration           time.Duration
	HeartbeatInterval       time.Duration
	RetryBaseDelay          time.Duration
	RetryMaxDelay           time.Duration
	MediaSweepInterval      time.Duration
	JobRetention            time.Duration
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		RecordingConcurrency:    1,
		GuestPreviewConcurrency: 1,
		ShadowingConcurrency:    2,
		CleanupConcurrency:      2,
		PollInterval:            time.Second,
		LeaseDuration:           2 * time.Minute,
		HeartbeatInterval:       30 * time.Second,
		RetryBaseDelay:          5 * time.Second,
		RetryMaxDelay:           5 * time.Minute,
		MediaSweepInterval:      15 * time.Minute,
		JobRetention:            30 * 24 * time.Hour,
	}
	var err error
	if config.RecordingConcurrency, err = positiveEnvInt("WORKER_RECORDING_CONCURRENCY", config.RecordingConcurrency); err != nil {
		return Config{}, err
	}
	if config.GuestPreviewConcurrency, err = positiveEnvInt("WORKER_GUEST_PREVIEW_CONCURRENCY", config.GuestPreviewConcurrency); err != nil {
		return Config{}, err
	}
	if config.ShadowingConcurrency, err = positiveEnvInt("WORKER_SHADOWING_CONCURRENCY", config.ShadowingConcurrency); err != nil {
		return Config{}, err
	}
	if config.CleanupConcurrency, err = positiveEnvInt("WORKER_CLEANUP_CONCURRENCY", config.CleanupConcurrency); err != nil {
		return Config{}, err
	}
	if config.PollInterval, err = positiveEnvDuration("WORKER_POLL_INTERVAL", config.PollInterval); err != nil {
		return Config{}, err
	}
	if config.LeaseDuration, err = positiveEnvDuration("WORKER_LEASE_DURATION", config.LeaseDuration); err != nil {
		return Config{}, err
	}
	if config.HeartbeatInterval, err = positiveEnvDuration("WORKER_HEARTBEAT_INTERVAL", config.HeartbeatInterval); err != nil {
		return Config{}, err
	}
	if config.RetryBaseDelay, err = positiveEnvDuration("WORKER_RETRY_BASE_DELAY", config.RetryBaseDelay); err != nil {
		return Config{}, err
	}
	if config.RetryMaxDelay, err = positiveEnvDuration("WORKER_RETRY_MAX_DELAY", config.RetryMaxDelay); err != nil {
		return Config{}, err
	}
	if config.MediaSweepInterval, err = positiveEnvDuration("MEDIA_SWEEP_INTERVAL", config.MediaSweepInterval); err != nil {
		return Config{}, err
	}
	if config.JobRetention, err = positiveEnvDuration("WORKER_JOB_RETENTION", config.JobRetention); err != nil {
		return Config{}, err
	}
	if config.JobRetention < 24*time.Hour {
		return Config{}, errors.New("WORKER_JOB_RETENTION must be at least 24h")
	}
	if config.HeartbeatInterval >= config.LeaseDuration {
		return Config{}, errors.New("WORKER_HEARTBEAT_INTERVAL must be shorter than WORKER_LEASE_DURATION")
	}
	return config, nil
}

func positiveEnvInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > 64 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 64", name)
	}
	return parsed, nil
}

func positiveEnvDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}
