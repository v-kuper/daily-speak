package workqueue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

type Handler func(context.Context, Job) error

type RunnerConfig struct {
	Kinds             []string
	Concurrency       int
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	RetryBaseDelay    time.Duration
	RetryMaxDelay     time.Duration
	Handle            Handler
	FinalizeFailure   TerminalFailureFunc
}

func (config RunnerConfig) withDefaults() RunnerConfig {
	if config.Concurrency <= 0 {
		config.Concurrency = 1
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = config.LeaseDuration / 3
	}
	if config.RetryBaseDelay <= 0 {
		config.RetryBaseDelay = 5 * time.Second
	}
	if config.RetryMaxDelay <= 0 {
		config.RetryMaxDelay = 5 * time.Minute
	}
	return config
}

func Run(ctx context.Context, store *Store, config RunnerConfig) error {
	config = config.withDefaults()
	if store == nil || config.Handle == nil || len(config.Kinds) == 0 {
		return errors.New("processing job runner is not configured")
	}
	errCh := make(chan error, config.Concurrency)
	for index := 0; index < config.Concurrency; index++ {
		owner := fmt.Sprintf("%s/%d/%s", config.Kinds[0], index, uuid.NewString())
		go func() {
			errCh <- runConsumer(ctx, store, owner, config)
		}()
	}
	for range config.Concurrency {
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return ctx.Err()
}

func runConsumer(ctx context.Context, store *Store, owner string, config RunnerConfig) error {
	ticker := time.NewTicker(config.PollInterval)
	defer ticker.Stop()
	for {
		job, found, err := store.Claim(ctx, owner, config.Kinds, config.LeaseDuration)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("processing job claim failed: %v", err)
		} else if found {
			runClaim(ctx, store, job, config)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runClaim(parent context.Context, store *Store, job Job, config RunnerConfig) {
	jobCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(config.HeartbeatInterval)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				if err := store.Heartbeat(jobCtx, job, config.LeaseDuration); err != nil {
					log.Printf("processing job heartbeat failed id=%s kind=%s: %v", job.ID, job.Kind, err)
					cancel()
					return
				}
			}
		}
	}()
	err := config.Handle(jobCtx, job)
	cancel()
	<-done
	if err == nil {
		if completeErr := store.Complete(context.WithoutCancel(parent), job); completeErr != nil && !errors.Is(completeErr, ErrLeaseLost) {
			log.Printf("processing job completion failed id=%s kind=%s: %v", job.ID, job.Kind, completeErr)
		}
		return
	}
	delay := RetryDelay(job.Attempts, config.RetryBaseDelay, config.RetryMaxDelay)
	terminal, failErr := store.Fail(context.WithoutCancel(parent), job, err, delay, config.FinalizeFailure)
	if failErr != nil {
		if !errors.Is(failErr, ErrLeaseLost) {
			log.Printf("processing job failure update failed id=%s kind=%s: %v", job.ID, job.Kind, failErr)
		}
		return
	}
	log.Printf("processing job failed id=%s kind=%s attempt=%d/%d terminal=%t: %v", job.ID, job.Kind, job.Attempts, job.MaxAttempts, terminal, err)
}
