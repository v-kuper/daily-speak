package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const legacyUploadsURLPrefix = "/uploads/"

type Cleanup struct {
	database     *db.DB
	mediaService *Service
	store        storage.Store
	legacy       storage.LegacyUploadRemover
}

func NewCleanup(database *db.DB, mediaService *Service, store storage.Store, legacy storage.LegacyUploadRemover) *Cleanup {
	return &Cleanup{
		database: database, mediaService: mediaService, store: store, legacy: legacy,
	}
}

func (cleanup *Cleanup) Available() bool {
	return cleanup != nil && cleanup.database != nil && cleanup.mediaService != nil
}

func (cleanup *Cleanup) AbortExpiredUploads(ctx context.Context) error {
	if !cleanup.Available() {
		return nil
	}
	type expiredUpload struct {
		ID      string
		OwnerID string
	}
	rows, err := cleanup.database.Query(ctx, `
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
	var uploads []expiredUpload
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
		_, err := cleanup.mediaService.AbortExpiredUpload(ctx, upload.OwnerID, upload.ID, 30*time.Minute)
		if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return nil
}

func (cleanup *Cleanup) EnqueueExpiredAssets(ctx context.Context) error {
	if !cleanup.Available() {
		return nil
	}
	tx, err := cleanup.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
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
	var assetIDs []string
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			rows.Close()
			return err
		}
		assetIDs = append(assetIDs, assetID)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return rowsErr
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

func (cleanup *Cleanup) Delete(ctx context.Context, resourceID string) error {
	if cleanup == nil || cleanup.database == nil {
		return errors.New("media cleanup database is not configured")
	}
	if strings.HasPrefix(resourceID, legacyUploadsURLPrefix) {
		if cleanup.legacy == nil {
			return errors.New("legacy media cleanup is not configured")
		}
		if err := cleanup.legacy.Remove([]string{resourceID}); err != nil {
			return err
		}
		_, err := cleanup.database.Exec(ctx, `DELETE FROM pending_file_deletions WHERE public_url = $1`, resourceID)
		return err
	}
	if cleanup.store == nil {
		return errors.New("media storage is not configured")
	}
	var driver, objectKey, state string
	err := cleanup.database.QueryRow(ctx, `
		SELECT storage_driver, object_key, state
		FROM media_assets
		WHERE id = $1`, resourceID).Scan(&driver, &objectKey, &state)
	if errors.Is(err, pgx.ErrNoRows) || state == "deleted" {
		return nil
	}
	if err != nil {
		return err
	}
	if driver != cleanup.store.Backend() {
		return fmt.Errorf("media asset requires %s storage, worker has %s", driver, cleanup.store.Backend())
	}
	if err := cleanup.store.Delete(ctx, objectKey); err != nil {
		return err
	}
	_, err = cleanup.database.Exec(ctx, `
		UPDATE media_assets
		SET state = 'deleted', deleted_at = COALESCE(deleted_at, NOW()), updated_at = NOW()
		WHERE id = $1`, resourceID)
	return err
}

func (cleanup *Cleanup) FinalizeFailure(ctx context.Context, tx pgx.Tx, resourceID string, attempts int, message string) error {
	if !strings.HasPrefix(resourceID, legacyUploadsURLPrefix) {
		_, err := tx.Exec(ctx, `
			UPDATE media_assets
			SET state = 'failed', updated_at = NOW()
			WHERE id = $1 AND state = 'deleting'`, resourceID)
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO pending_file_deletions (public_url, attempts, last_error, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (public_url) DO UPDATE
		SET attempts = EXCLUDED.attempts, last_error = EXCLUDED.last_error, updated_at = NOW()`,
		resourceID, attempts, truncateCleanupMessage(message, 500))
	return err
}

func truncateCleanupMessage(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
