package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/operations"
	"daily-speaking-practice/backend/internal/worker"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
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
	if s.db == nil || s.mediaService == nil {
		return nil
	}
	type expiredUpload struct {
		ID      string
		OwnerID string
	}
	rows, err := s.db.Query(ctx, `
		SELECT u.id, a.owner_principal_id
		FROM media_uploads u
		JOIN media_assets a ON a.id = u.asset_id
		WHERE u.expires_at <= NOW()
		  AND (
		    u.state IN ('pending', 'uploading', 'failed', 'aborting')
		    OR (u.state = 'completing' AND u.updated_at <= NOW() - INTERVAL '30 minutes')
		  )
		ORDER BY u.expires_at ASC
		LIMIT 100`)
	if err != nil {
		return err
	}
	uploads := make([]expiredUpload, 0, 100)
	for rows.Next() {
		var upload expiredUpload
		if err := rows.Scan(&upload.ID, &upload.OwnerID); err != nil {
			rows.Close()
			return err
		}
		uploads = append(uploads, upload)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return rowsErr
	}
	for _, upload := range uploads {
		_, abortErr := s.mediaService.AbortExpiredUpload(ctx, upload.OwnerID, upload.ID, 30*time.Minute)
		if abortErr != nil && !errors.Is(abortErr, media.ErrNotFound) && !errors.Is(abortErr, media.ErrConflict) {
			return abortErr
		}
	}
	if err := s.expireGuestPreviews(ctx); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	assetRows, err := tx.Query(ctx, `
		SELECT id
		FROM media_assets
		WHERE attached_at IS NULL
		  AND retention_until <= NOW()
		  AND state IN ('ready', 'failed')
		ORDER BY retention_until ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 100`)
	if err != nil {
		return err
	}
	assetIDs := make([]string, 0, 100)
	for assetRows.Next() {
		var assetID string
		if err := assetRows.Scan(&assetID); err != nil {
			assetRows.Close()
			return err
		}
		assetIDs = append(assetIDs, assetID)
	}
	assetRowsErr := assetRows.Err()
	assetRows.Close()
	if assetRowsErr != nil {
		return assetRowsErr
	}
	for _, assetID := range assetIDs {
		result, err := tx.Exec(ctx, `
			UPDATE media_assets
			SET state = 'deleting', updated_at = NOW()
			WHERE id = $1 AND attached_at IS NULL AND state IN ('ready', 'failed')`, assetID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			continue
		}
		jobID := uuid.NewString()
		if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
			ID: jobID, Kind: workqueue.KindMediaDelete, ResourceID: assetID,
			IdempotencyKey: "media.expire:asset:" + assetID + ":" + jobID, MaxAttempts: 20,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
		if strings.HasPrefix(job.ResourceID, uploadsURLPrefix) {
			if err := s.removeStoredUploads([]string{job.ResourceID}); err != nil {
				return err
			}
			_, err := s.db.Exec(jobCtx, `DELETE FROM pending_file_deletions WHERE public_url = $1`, job.ResourceID)
			return err
		}
		if s.mediaStore == nil {
			return errors.New("media storage is not configured")
		}
		var driver, objectKey, state string
		err := s.db.QueryRow(jobCtx, `
			SELECT storage_driver, object_key, state
			FROM media_assets
			WHERE id = $1`, job.ResourceID).Scan(&driver, &objectKey, &state)
		if errors.Is(err, pgx.ErrNoRows) || state == "deleted" {
			return nil
		}
		if err != nil {
			return err
		}
		if driver != s.mediaStore.Backend() {
			return fmt.Errorf("media asset requires %s storage, worker has %s", driver, s.mediaStore.Backend())
		}
		if err := s.mediaStore.Delete(jobCtx, objectKey); err != nil {
			return err
		}
		_, err = s.db.Exec(jobCtx, `
			UPDATE media_assets
			SET state = 'deleted', deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
			WHERE id = $1`, job.ResourceID)
		return err
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
		if !strings.HasPrefix(job.ResourceID, uploadsURLPrefix) {
			_, err := tx.Exec(ctx, `
				UPDATE media_assets
				SET state = 'failed', updated_at = NOW()
				WHERE id = $1 AND state = 'deleting'`, job.ResourceID)
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO pending_file_deletions (public_url, attempts, last_error, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (public_url) DO UPDATE
			SET attempts = EXCLUDED.attempts, last_error = EXCLUDED.last_error, updated_at = NOW()`,
			job.ResourceID, job.Attempts, truncateRunes(message, 500))
		return err
	default:
		return nil
	}
}
