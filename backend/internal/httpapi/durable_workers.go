package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/worker"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

func (s *Server) RunWorkers(ctx context.Context, config worker.Config) error {
	if s.db == nil || s.jobStore == nil {
		return errors.New("worker database is not configured")
	}
	return worker.Run(ctx, s.jobStore, config, worker.Processor{
		Handle:          s.handleDurableJob,
		FinalizeFailure: s.finalizeDurableFailure,
		Maintain:        s.performWorkerMaintenance,
	})
}

func (s *Server) performWorkerMaintenance(ctx context.Context, jobRetention time.Duration) {
	logger := logging.ForBackground("worker.media.maintenance")
	if err := s.sweepExpiredMedia(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("media.sweep_failed", logging.ErrorMeta(err))
	}
	if s.jobStore != nil && jobRetention > 0 {
		removed, err := s.jobStore.PruneTerminal(ctx, time.Now().UTC().Add(-jobRetention), 5000)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("jobs.prune_failed", logging.ErrorMeta(err))
		} else if removed > 0 {
			logger.Info("jobs.pruned", map[string]any{"count": removed})
		}
	}
	if s.db != nil {
		removed, err := operations.PruneExpiredRateLimits(ctx, s.db, time.Now().UTC(), 5000)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("rate_limits.prune_failed", logging.ErrorMeta(err))
		} else if removed > 0 {
			logger.Info("rate_limits.pruned", map[string]any{"count": removed})
		}
	}
}

func (s *Server) sweepExpiredMedia(ctx context.Context) error {
	cleanup := media.NewCleanup(s.db, s.mediaService, s.mediaStore, s.removeStoredUploads)
	if !cleanup.Available() {
		return nil
	}
	if err := cleanup.AbortExpiredUploads(ctx); err != nil {
		return err
	}
	if err := s.expireGuestPreviews(ctx); err != nil {
		return err
	}
	return cleanup.EnqueueExpiredAssets(ctx)
}

func (s *Server) handleDurableJob(ctx context.Context, job workqueue.Job) error {
	switch job.Kind {
	case workqueue.KindGuestPreview:
		jobCtx, cancel := context.WithTimeout(ctx, guestPreviewProcessingTimeout)
		defer cancel()
		return s.runGuestPreviewJob(jobCtx, job)
	case workqueue.KindRecordingProcess:
		jobCtx, cancel := context.WithTimeout(ctx, recordingProcessingTimeout)
		defer cancel()
		return s.runRecordingJob(jobCtx, job)
	case workqueue.KindShadowingSynthesize:
		jobCtx, cancel := context.WithTimeout(ctx, shadowingJobTimeout)
		defer cancel()
		return s.runShadowingJob(jobCtx, job)
	case workqueue.KindMediaDelete:
		jobCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return media.NewCleanup(s.db, s.mediaService, s.mediaStore, s.removeStoredUploads).Delete(jobCtx, job.ResourceID)
	default:
		return fmt.Errorf("unsupported processing job kind %q", job.Kind)
	}
}

func (s *Server) finalizeDurableFailure(ctx context.Context, tx pgx.Tx, job workqueue.Job, message string) error {
	switch job.Kind {
	case workqueue.KindGuestPreview:
		_, err := tx.Exec(ctx, `
			UPDATE guest_previews
			SET state = 'failed', processing_error = $3, updated_at = NOW()
			WHERE id = $1 AND preview_job_id = $2 AND state IN ('queued', 'processing')`,
			job.ResourceID, job.ID, truncateRunes(message, 500))
		return err
	case workqueue.KindRecordingProcess:
		_, err := tx.Exec(ctx, `
			UPDATE recordings
			SET status = 'failed', processing_error = $3
			WHERE id = $1 AND status = 'processing' AND processing_job_id = $2`,
			job.ResourceID, job.ID, truncateRunes(message, 500))
		return err
	case workqueue.KindShadowingSynthesize:
		_, err := tx.Exec(ctx, `
			UPDATE recordings
			SET shadowing_status = 'failed', shadowing_audio_url = NULL,
			    shadowing_error = $3, shadowing_updated_at = NOW(), shadowing_attempt_id = NULL
			WHERE id = $1 AND shadowing_status = 'processing' AND shadowing_attempt_id = $2`,
			job.ResourceID, job.ID, shadowingFailureMessage)
		return err
	case workqueue.KindMediaDelete:
		return media.NewCleanup(s.db, s.mediaService, s.mediaStore, s.removeStoredUploads).
			FinalizeFailure(ctx, tx, job.ResourceID, job.Attempts, message)
	default:
		return nil
	}
}
