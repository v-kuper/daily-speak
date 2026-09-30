package recording

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/jackc/pgx/v5"
)

type SQLDeletionRepository struct{ db *db.DB }

func NewSQLDeletionRepository(database *db.DB) *SQLDeletionRepository {
	return &SQLDeletionRepository{db: database}
}

func (r *SQLDeletionRepository) Execute(ctx context.Context, operation func(DeletionTransaction) error) error {
	if r == nil || r.db == nil {
		return errors.New("recording database is not configured")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := operation(&sqlDeletionTransaction{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SQLDeletionRepository) Get(ctx context.Context, userID string, subscriber bool) (quota.RecordingQuota, error) {
	return quota.GetRecordingQuota(ctx, r.db, userID, &subscriber)
}

type sqlDeletionTransaction struct{ tx pgx.Tx }

func (t *sqlDeletionTransaction) Load(ctx context.Context, userID string, recordingID string) (DeletionSource, bool, error) {
	var source DeletionSource
	var recordingAudioAssetID, recordingPhotoAssetID, shadowingAssetID *string
	err := t.tx.QueryRow(ctx, `
		SELECT audio_asset_id, photo_asset_id, shadowing_asset_id
		FROM recordings
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, recordingID, userID).Scan(
		&recordingAudioAssetID, &recordingPhotoAssetID, &shadowingAssetID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeletionSource{}, false, nil
	}
	if err != nil {
		return DeletionSource{}, false, err
	}
	source.AssetIDs = append(source.AssetIDs, recordingAudioAssetID, recordingPhotoAssetID, shadowingAssetID)
	return source, true, nil
}

func (t *sqlDeletionTransaction) QueueAsset(ctx context.Context, assetID string, jobID string) error {
	if _, err := t.tx.Exec(ctx, `
		UPDATE media_assets
		SET state = 'deleting', retention_until = NOW(), updated_at = NOW()
		WHERE id = $1 AND state <> 'deleted'`, assetID); err != nil {
		return err
	}
	return workqueue.Enqueue(ctx, t.tx, workqueue.NewJob{
		ID: jobID, Kind: workqueue.KindMediaDelete, ResourceID: assetID,
		IdempotencyKey: "media.delete:asset:" + assetID, MaxAttempts: 20,
	})
}

func (t *sqlDeletionTransaction) Remove(ctx context.Context, userID string, recordingID string) (bool, error) {
	if _, err := t.tx.Exec(ctx, `
		UPDATE processing_jobs
		SET state = 'cancelled', completed_at = NOW(), updated_at = NOW(),
		    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
		WHERE resource_id = $1
		  AND kind IN ('recording.process', 'recording.strengths', 'shadowing.synthesize')
		  AND state IN ('queued', 'running', 'retry_wait')`, recordingID); err != nil {
		return false, err
	}
	if _, err := t.tx.Exec(ctx, `DELETE FROM interview_sessions
		WHERE recording_id=$1 AND EXISTS (
			SELECT 1 FROM recordings WHERE id=$1 AND user_id=$2
		)`, recordingID, userID); err != nil {
		return false, err
	}
	var deletedID string
	err := t.tx.QueryRow(ctx, `
		DELETE FROM recordings
		WHERE id = $1 AND user_id = $2
		RETURNING id`, recordingID, userID).Scan(&deletedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
