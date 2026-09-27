package httpapi

import (
	"context"
	"time"

	"daily-speaking-practice/backend/internal/worker"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

// These adapters remain for integration tests and backwards-compatible
// embedding. The production worker composes internal/background directly and
// has no dependency on HTTP transport.
func (s *Server) RunWorkers(ctx context.Context, config worker.Config) error {
	return s.backgroundRuntime.Run(ctx, config)
}

func (s *Server) performWorkerMaintenance(ctx context.Context, jobRetention time.Duration) {
	s.backgroundRuntime.Maintain(ctx, jobRetention)
}

func (s *Server) sweepExpiredMedia(ctx context.Context) error {
	return s.backgroundRuntime.Sweep(ctx)
}

func (s *Server) handleDurableJob(ctx context.Context, job workqueue.Job) error {
	return s.backgroundRuntime.Handle(ctx, job)
}

func (s *Server) finalizeDurableFailure(ctx context.Context, tx pgx.Tx, job workqueue.Job, message string) error {
	return s.backgroundRuntime.FinalizeFailure(ctx, tx, job, message)
}
