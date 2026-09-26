package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WorkerConfig struct {
	RecordingConcurrency int
	ShadowingConcurrency int
	CleanupConcurrency   int
	PollInterval         time.Duration
	LeaseDuration        time.Duration
	HeartbeatInterval    time.Duration
	RetryBaseDelay       time.Duration
	RetryMaxDelay        time.Duration
	MediaSweepInterval   time.Duration
}

func WorkerConfigFromEnv() (WorkerConfig, error) {
	config := WorkerConfig{
		RecordingConcurrency: 1,
		ShadowingConcurrency: 2,
		CleanupConcurrency:   2,
		PollInterval:         time.Second,
		LeaseDuration:        2 * time.Minute,
		HeartbeatInterval:    30 * time.Second,
		RetryBaseDelay:       5 * time.Second,
		RetryMaxDelay:        5 * time.Minute,
		MediaSweepInterval:   15 * time.Minute,
	}
	var err error
	if config.RecordingConcurrency, err = positiveEnvInt("WORKER_RECORDING_CONCURRENCY", config.RecordingConcurrency); err != nil {
		return WorkerConfig{}, err
	}
	if config.ShadowingConcurrency, err = positiveEnvInt("WORKER_SHADOWING_CONCURRENCY", config.ShadowingConcurrency); err != nil {
		return WorkerConfig{}, err
	}
	if config.CleanupConcurrency, err = positiveEnvInt("WORKER_CLEANUP_CONCURRENCY", config.CleanupConcurrency); err != nil {
		return WorkerConfig{}, err
	}
	if config.PollInterval, err = positiveEnvDuration("WORKER_POLL_INTERVAL", config.PollInterval); err != nil {
		return WorkerConfig{}, err
	}
	if config.LeaseDuration, err = positiveEnvDuration("WORKER_LEASE_DURATION", config.LeaseDuration); err != nil {
		return WorkerConfig{}, err
	}
	if config.HeartbeatInterval, err = positiveEnvDuration("WORKER_HEARTBEAT_INTERVAL", config.HeartbeatInterval); err != nil {
		return WorkerConfig{}, err
	}
	if config.RetryBaseDelay, err = positiveEnvDuration("WORKER_RETRY_BASE_DELAY", config.RetryBaseDelay); err != nil {
		return WorkerConfig{}, err
	}
	if config.RetryMaxDelay, err = positiveEnvDuration("WORKER_RETRY_MAX_DELAY", config.RetryMaxDelay); err != nil {
		return WorkerConfig{}, err
	}
	if config.MediaSweepInterval, err = positiveEnvDuration("MEDIA_SWEEP_INTERVAL", config.MediaSweepInterval); err != nil {
		return WorkerConfig{}, err
	}
	if config.HeartbeatInterval >= config.LeaseDuration {
		return WorkerConfig{}, errors.New("WORKER_HEARTBEAT_INTERVAL must be shorter than WORKER_LEASE_DURATION")
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

func (s *Server) RunWorkers(ctx context.Context, config WorkerConfig) error {
	if s.db == nil || s.jobStore == nil {
		return errors.New("worker database is not configured")
	}
	pools := []workqueue.RunnerConfig{
		{
			Kinds:             []string{workqueue.KindRecordingProcess},
			Concurrency:       config.RecordingConcurrency,
			PollInterval:      config.PollInterval,
			LeaseDuration:     config.LeaseDuration,
			HeartbeatInterval: config.HeartbeatInterval,
			RetryBaseDelay:    config.RetryBaseDelay,
			RetryMaxDelay:     config.RetryMaxDelay,
			Handle:            s.handleDurableJob,
			FinalizeFailure:   s.finalizeDurableFailure,
		},
		{
			Kinds:             []string{workqueue.KindShadowingSynthesize},
			Concurrency:       config.ShadowingConcurrency,
			PollInterval:      config.PollInterval,
			LeaseDuration:     config.LeaseDuration,
			HeartbeatInterval: config.HeartbeatInterval,
			RetryBaseDelay:    config.RetryBaseDelay,
			RetryMaxDelay:     config.RetryMaxDelay,
			Handle:            s.handleDurableJob,
			FinalizeFailure:   s.finalizeDurableFailure,
		},
		{
			Kinds:             []string{workqueue.KindMediaDelete},
			Concurrency:       config.CleanupConcurrency,
			PollInterval:      config.PollInterval,
			LeaseDuration:     config.LeaseDuration,
			HeartbeatInterval: config.HeartbeatInterval,
			RetryBaseDelay:    config.RetryBaseDelay,
			RetryMaxDelay:     config.RetryMaxDelay,
			Handle:            s.handleDurableJob,
			FinalizeFailure:   s.finalizeDurableFailure,
		},
	}
	errCh := make(chan error, len(pools)+1)
	var wait sync.WaitGroup
	for _, pool := range pools {
		pool := pool
		wait.Add(1)
		go func() {
			defer wait.Done()
			errCh <- workqueue.Run(ctx, s.jobStore, pool)
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		errCh <- s.runMediaMaintenanceLoop(ctx, config.MediaSweepInterval)
	}()
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return ctx.Err()
}

func (s *Server) runMediaMaintenanceLoop(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	logger := logging.ForBackground("worker.media.maintenance")
	run := func() {
		if err := s.sweepExpiredMedia(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("media.sweep_failed", logging.ErrorMeta(err))
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			run()
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
