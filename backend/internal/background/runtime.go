package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
	"daily-speaking-practice/backend/internal/worker"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type RecordingProcessor interface {
	Process(context.Context, recording.ProcessingJob, recording.AnalysisLogger) error
}

type GuestPreviewProcessor interface {
	Process(context.Context, guestpreview.Job) error
}

type ShadowingProcessor interface {
	Process(context.Context, shadowing.Job, shadowing.Logger) error
}

type InterviewProcessor interface {
	Process(context.Context, workqueue.Job) error
}

type InterviewStore interface {
	Expire(context.Context) error
	FinalizeFailure(context.Context, pgx.Tx, workqueue.Job, string) error
}

type RecordingFinalizer interface {
	FinalizeFailure(context.Context, pgx.Tx, string, string, string) error
}

type GuestPreviewStore interface {
	Expire(context.Context) error
	FinalizeFailure(context.Context, pgx.Tx, string, string, string) error
}

type ShadowingFinalizer interface {
	FinalizeFailure(context.Context, pgx.Tx, string, string) error
}

type MediaCleanup interface {
	Available() bool
	AbortExpiredUploads(context.Context) error
	EnqueueExpiredAssets(context.Context) error
	Delete(context.Context, string) error
	FinalizeFailure(context.Context, pgx.Tx, string, int, string) error
}

type Dependencies struct {
	DB                    *db.DB
	JobStore              *workqueue.Store
	RecordingProcessor    RecordingProcessor
	RecordingRepository   RecordingFinalizer
	GuestPreviewProcessor GuestPreviewProcessor
	GuestPreviewStore     GuestPreviewStore
	InterviewProcessor    InterviewProcessor
	InterviewStore        InterviewStore
	ShadowingProcessor    ShadowingProcessor
	ShadowingStore        ShadowingFinalizer
	MediaCleanup          MediaCleanup
}

type Runtime struct{ dependencies Dependencies }

func NewRuntime(dependencies Dependencies) *Runtime {
	return &Runtime{dependencies: dependencies}
}

func (r *Runtime) Run(ctx context.Context, config worker.Config) error {
	if r == nil || r.dependencies.DB == nil || r.dependencies.JobStore == nil {
		return errors.New("worker database is not configured")
	}
	return worker.Run(ctx, r.dependencies.JobStore, config, worker.Processor{
		Handle: r.Handle, FinalizeFailure: r.FinalizeFailure, Maintain: r.Maintain,
	})
}

func (r *Runtime) Handle(ctx context.Context, job workqueue.Job) error {
	if r == nil {
		return errors.New("worker runtime is not configured")
	}
	switch job.Kind {
	case workqueue.KindGuestPreview:
		if r.dependencies.GuestPreviewProcessor == nil {
			return errors.New("guest preview processor is not configured")
		}
		jobCtx, cancel := context.WithTimeout(ctx, guestpreview.ProcessingTimeout)
		defer cancel()
		return r.dependencies.GuestPreviewProcessor.Process(jobCtx, guestpreview.Job{ID: job.ID, ResourceID: job.ResourceID, LeaseToken: job.LeaseToken})
	case workqueue.KindRecordingProcess:
		if r.dependencies.RecordingProcessor == nil {
			return errors.New("recording processor is not configured")
		}
		jobCtx, cancel := context.WithTimeout(ctx, recording.ProcessingTimeout)
		defer cancel()
		return r.dependencies.RecordingProcessor.Process(jobCtx, recording.ProcessingJob{ID: job.ID, ResourceID: job.ResourceID, LeaseToken: job.LeaseToken}, logging.ForBackground("worker.recordings.process"))
	case workqueue.KindInterviewProcess:
		if r.dependencies.InterviewProcessor == nil {
			return errors.New("interview processor is not configured")
		}
		jobCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		return r.dependencies.InterviewProcessor.Process(jobCtx, job)
	case workqueue.KindShadowingSynthesize:
		if r.dependencies.ShadowingProcessor == nil {
			return errors.New("shadowing processor is not configured")
		}
		jobCtx, cancel := context.WithTimeout(ctx, shadowing.JobTimeout)
		defer cancel()
		return r.dependencies.ShadowingProcessor.Process(jobCtx, shadowing.Job{ID: job.ID, ResourceID: job.ResourceID, LeaseToken: job.LeaseToken}, logging.ForBackground("worker.recordings.shadowing"))
	case workqueue.KindMediaDelete:
		if r.dependencies.MediaCleanup == nil {
			return errors.New("media cleanup is not configured")
		}
		jobCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return r.dependencies.MediaCleanup.Delete(jobCtx, job.ResourceID)
	default:
		return fmt.Errorf("unsupported processing job kind %q", job.Kind)
	}
}

func (r *Runtime) FinalizeFailure(ctx context.Context, tx pgx.Tx, job workqueue.Job, message string) error {
	if r == nil {
		return errors.New("worker runtime is not configured")
	}
	switch job.Kind {
	case workqueue.KindGuestPreview:
		if r.dependencies.GuestPreviewStore == nil {
			return errors.New("guest preview store is not configured")
		}
		return r.dependencies.GuestPreviewStore.FinalizeFailure(ctx, tx, job.ID, job.ResourceID, message)
	case workqueue.KindRecordingProcess:
		if r.dependencies.RecordingRepository == nil {
			return errors.New("recording repository is not configured")
		}
		return r.dependencies.RecordingRepository.FinalizeFailure(ctx, tx, job.ID, job.ResourceID, message)
	case workqueue.KindInterviewProcess:
		if r.dependencies.InterviewStore == nil {
			return errors.New("interview store is not configured")
		}
		return r.dependencies.InterviewStore.FinalizeFailure(ctx, tx, job, message)
	case workqueue.KindShadowingSynthesize:
		if r.dependencies.ShadowingStore == nil {
			return errors.New("shadowing store is not configured")
		}
		return r.dependencies.ShadowingStore.FinalizeFailure(ctx, tx, job.ID, job.ResourceID)
	case workqueue.KindMediaDelete:
		if r.dependencies.MediaCleanup == nil {
			return errors.New("media cleanup is not configured")
		}
		return r.dependencies.MediaCleanup.FinalizeFailure(ctx, tx, job.ResourceID, job.Attempts, message)
	default:
		return nil
	}
}

func (r *Runtime) Maintain(ctx context.Context, jobRetention time.Duration) {
	logger := logging.ForBackground("worker.maintenance")
	if err := r.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("media.sweep_failed", logging.ErrorMeta(err))
	}
	if r.dependencies.JobStore != nil && jobRetention > 0 {
		removed, err := r.dependencies.JobStore.PruneTerminal(ctx, time.Now().UTC().Add(-jobRetention), 5000)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("jobs.prune_failed", logging.ErrorMeta(err))
		} else if removed > 0 {
			logger.Info("jobs.pruned", map[string]any{"count": removed})
		}
	}
	if r.dependencies.DB != nil {
		removed, err := operations.PruneExpiredRateLimits(ctx, r.dependencies.DB, time.Now().UTC(), 5000)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("rate_limits.prune_failed", logging.ErrorMeta(err))
		} else if removed > 0 {
			logger.Info("rate_limits.pruned", map[string]any{"count": removed})
		}
	}
}

func (r *Runtime) Sweep(ctx context.Context) error {
	if r == nil || r.dependencies.MediaCleanup == nil || !r.dependencies.MediaCleanup.Available() {
		return nil
	}
	if err := r.dependencies.MediaCleanup.AbortExpiredUploads(ctx); err != nil {
		return err
	}
	if r.dependencies.GuestPreviewStore != nil {
		if err := r.dependencies.GuestPreviewStore.Expire(ctx); err != nil {
			return err
		}
	}
	if r.dependencies.InterviewStore != nil {
		if err := r.dependencies.InterviewStore.Expire(ctx); err != nil {
			return err
		}
	}
	return r.dependencies.MediaCleanup.EnqueueExpiredAssets(ctx)
}
