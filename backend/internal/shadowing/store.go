package shadowing

import (
	"context"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Store struct {
	db    *db.DB
	newID func() string
}

func NewStore(database *db.DB) *Store { return &Store{db: database, newID: uuid.NewString} }

func (s *Store) Schedule(ctx context.Context, userID, recordingID string) (bool, error) {
	userID, recordingID = strings.TrimSpace(userID), strings.TrimSpace(recordingID)
	attemptID := s.newID()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var correctedTranscript string
	err = tx.QueryRow(ctx, `
		UPDATE recordings
		SET shadowing_status = 'processing', shadowing_error = NULL,
		    shadowing_updated_at = NOW(), shadowing_attempt_id = $3
		WHERE id = $1 AND user_id = $2 AND BTRIM(corrected_transcript) <> ''
		  AND (shadowing_status IN ('pending', 'failed')
		    OR (shadowing_status = 'processing' AND shadowing_attempt_id IS NULL AND shadowing_updated_at < NOW() - INTERVAL '5 minutes'))
		RETURNING corrected_transcript`, recordingID, userID, attemptID).Scan(&correctedTranscript)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.db.QueryRow(ctx, `SELECT corrected_transcript FROM recordings WHERE id = $1 AND user_id = $2`, recordingID, userID).Scan(&correctedTranscript); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, ErrNotFound
			}
			return false, err
		}
		if strings.TrimSpace(correctedTranscript) == "" {
			return false, ErrTranscriptUnavailable
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: attemptID, Kind: workqueue.KindShadowingSynthesize, ResourceID: recordingID, IdempotencyKey: "shadowing:" + attemptID, MaxAttempts: 4}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) LoadWork(ctx context.Context, job Job) (Work, bool, error) {
	var work Work
	err := s.db.QueryRow(ctx, `
		SELECT user_id, corrected_transcript FROM recordings
		WHERE id = $1 AND shadowing_status = 'processing' AND shadowing_attempt_id = $2`,
		job.ResourceID, job.ID).Scan(&work.UserID, &work.CorrectedTranscript)
	if errors.Is(err, pgx.ErrNoRows) {
		return Work{}, false, nil
	}
	return work, err == nil, err
}

func (s *Store) Complete(ctx context.Context, job Job, asset Asset) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var bucket any
	if asset.Bucket != "" {
		bucket = asset.Bucket
	}
	var legacyURL any
	if asset.LegacyPublicURL != "" {
		legacyURL = asset.LegacyPublicURL
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, bucket, object_key,
		   content_type, expected_size_bytes, verified_size_bytes,
		   expected_checksum_sha256, verified_checksum_sha256, etag,
		   legacy_public_url, verified_at, attached_at)
		VALUES ($1, $2, 'shadowing_audio', 'ready', $3, $4, $5,
		   'audio/mpeg', $6, $6, $7, $7, NULLIF($8, ''), $9, NOW(), NOW())`,
		asset.ID, asset.OwnerID, asset.StorageDriver, bucket, asset.ObjectKey, asset.Size,
		asset.Checksum, asset.ETag, legacyURL)
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE recordings
		SET shadowing_status = 'ready', shadowing_audio_url = $2, shadowing_asset_id = $6,
		    shadowing_error = NULL, shadowing_updated_at = NOW(), shadowing_attempt_id = NULL
		WHERE id = $1 AND user_id = $3 AND shadowing_status = 'processing' AND shadowing_attempt_id = $4
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $4 AND state = 'running' AND lease_token = $5)`,
		job.ResourceID, nullableString(asset.LegacyPublicURL), asset.OwnerID, job.ID, job.LeaseToken, asset.ID)
	if err != nil || result.RowsAffected() == 0 {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) FinalizeFailure(ctx context.Context, tx pgx.Tx, jobID, recordingID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE recordings SET shadowing_status = 'failed', shadowing_audio_url = NULL,
		    shadowing_error = $3, shadowing_updated_at = NOW(), shadowing_attempt_id = NULL
		WHERE id = $1 AND shadowing_status = 'processing' AND shadowing_attempt_id = $2`,
		recordingID, jobID, FailureMessage)
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
