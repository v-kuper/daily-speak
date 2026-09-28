package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"daily-speaking-practice/backend/internal/workqueue"
)

type Processor struct {
	Handle          workqueue.Handler
	FinalizeFailure workqueue.TerminalFailureFunc
	Maintain        func(context.Context, time.Duration)
}

func Run(ctx context.Context, store *workqueue.Store, config Config, processor Processor) error {
	if store == nil || processor.Handle == nil || processor.FinalizeFailure == nil {
		return errors.New("worker runtime is not configured")
	}
	pools := []workqueue.RunnerConfig{
		poolConfig(config, []string{workqueue.KindGuestPreview}, config.GuestPreviewConcurrency, processor),
		poolConfig(config, []string{workqueue.KindRecordingProcess}, config.RecordingConcurrency, processor),
		poolConfig(config, []string{workqueue.KindInterviewProcess}, config.InterviewConcurrency, processor),
		poolConfig(config, []string{workqueue.KindShadowingSynthesize}, config.ShadowingConcurrency, processor),
		poolConfig(config, []string{workqueue.KindMediaDelete}, config.CleanupConcurrency, processor),
	}

	errCh := make(chan error, len(pools)+1)
	var wait sync.WaitGroup
	for _, pool := range pools {
		pool := pool
		wait.Add(1)
		go func() {
			defer wait.Done()
			errCh <- workqueue.Run(ctx, store, pool)
		}()
	}
	if processor.Maintain != nil {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errCh <- runMaintenanceLoop(ctx, config, processor.Maintain)
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return ctx.Err()
}

func poolConfig(config Config, kinds []string, concurrency int, processor Processor) workqueue.RunnerConfig {
	return workqueue.RunnerConfig{
		Kinds:             kinds,
		Concurrency:       concurrency,
		PollInterval:      config.PollInterval,
		LeaseDuration:     config.LeaseDuration,
		HeartbeatInterval: config.HeartbeatInterval,
		RetryBaseDelay:    config.RetryBaseDelay,
		RetryMaxDelay:     config.RetryMaxDelay,
		Handle:            processor.Handle,
		FinalizeFailure:   processor.FinalizeFailure,
	}
}

func runMaintenanceLoop(ctx context.Context, config Config, maintain func(context.Context, time.Duration)) error {
	interval := config.MediaSweepInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	maintain(ctx, config.JobRetention)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			maintain(ctx, config.JobRetention)
		}
	}
}
