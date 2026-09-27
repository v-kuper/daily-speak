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
	var recordingAudioURL, shadowingAudioURL, recordingAudioAssetID, recordingPhotoAssetID, shadowingAssetID *string
	err := t.tx.QueryRow(ctx, `
		SELECT audio_data_url, shadowing_audio_url, audio_asset_id, photo_asset_id, shadowing_asset_id
		FROM recordings
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, recordingID, userID).Scan(
		&recordingAudioURL, &shadowingAudioURL, &recordingAudioAssetID,
		&recordingPhotoAssetID, &shadowingAssetID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeletionSource{}, false, nil
	}
	if err != nil {
		return DeletionSource{}, false, err
	}
	source.LegacyURLs = append(source.LegacyURLs, recordingAudioURL, shadowingAudioURL)
	source.AssetIDs = append(source.AssetIDs, recordingAudioAssetID, recordingPhotoAssetID, shadowingAssetID)

	postIDs := []string{}
	postRows, err := t.tx.Query(ctx, `
		SELECT id, audio_data_url, audio_asset_id, photo_asset_id
		FROM feed_posts
		WHERE source_recording_id = $1
		FOR UPDATE`, recordingID)
	if err != nil {
		return DeletionSource{}, false, err
	}
	for postRows.Next() {
		var postID string
		var audioURL, audioAssetID, photoAssetID *string
		if err := postRows.Scan(&postID, &audioURL, &audioAssetID, &photoAssetID); err != nil {
			return DeletionSource{}, false, err
		}
		postIDs = append(postIDs, postID)
		source.LegacyURLs = append(source.LegacyURLs, audioURL)
		source.AssetIDs = append(source.AssetIDs, audioAssetID, photoAssetID)
	}
	postRowsErr := postRows.Err()
	postRows.Close()
	if postRowsErr != nil {
		return DeletionSource{}, false, postRowsErr
	}
	if len(postIDs) == 0 {
		return source, true, nil
	}
	replyRows, err := t.tx.Query(ctx, `
		SELECT audio_data_url, audio_asset_id
		FROM feed_replies
		WHERE post_id = ANY($1::text[])
		FOR UPDATE`, postIDs)
	if err != nil {
		return DeletionSource{}, false, err
	}
	for replyRows.Next() {
		var audioURL, audioAssetID *string
		if err := replyRows.Scan(&audioURL, &audioAssetID); err != nil {
			return DeletionSource{}, false, err
		}
		source.LegacyURLs = append(source.LegacyURLs, audioURL)
		source.AssetIDs = append(source.AssetIDs, audioAssetID)
	}
	replyRowsErr := replyRows.Err()
	replyRows.Close()
	return source, true, replyRowsErr
}

func (t *sqlDeletionTransaction) QueueLegacyMedia(ctx context.Context, publicURL string, jobID string) error {
	if _, err := t.tx.Exec(ctx, `
		INSERT INTO pending_file_deletions (public_url)
		VALUES ($1)
		ON CONFLICT (public_url) DO NOTHING`, publicURL); err != nil {
		return err
	}
	return workqueue.Enqueue(ctx, t.tx, workqueue.NewJob{
		ID: jobID, Kind: workqueue.KindMediaDelete, ResourceID: publicURL,
		IdempotencyKey: "media.delete:" + publicURL, MaxAttempts: 20,
	})
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
		  AND kind IN ('recording.process', 'shadowing.synthesize')
		  AND state IN ('queued', 'running', 'retry_wait')`, recordingID); err != nil {
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
