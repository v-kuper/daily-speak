package guestpreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct {
	db            *db.DB
	queueCapacity int
	newID         func() string
}

func NewStore(database *db.DB, queueCapacity int) *Store {
	if queueCapacity < 1 {
		queueCapacity = DefaultQueueCapacity
	}
	return &Store{db: database, queueCapacity: queueCapacity, newID: uuid.NewString}
}

func (s *Store) Create(ctx context.Context, principalID, idempotencyKey, requestDigest string, input CreateRequest, timestamp time.Time) (Preview, bool, error) {
	if existing, err := s.Find(ctx, principalID, "", idempotencyKey); err == nil {
		if existing.RequestDigest != requestDigest {
			return Preview{}, false, ErrConflict
		}
		if !existing.ExpiresAt.After(time.Now().UTC()) || existing.State == "promoted" {
			return Preview{}, false, ErrNotFound
		}
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Preview{}, false, err
	}

	now := time.Now().UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Preview{}, false, err
	}
	defer tx.Rollback(ctx)
	var principalExpires time.Time
	if err := tx.QueryRow(ctx, `
		SELECT expires_at FROM principals
		WHERE id = $1 AND kind = 'guest' AND merged_into_principal_id IS NULL AND expires_at > $2
		FOR UPDATE`, principalID, now).Scan(&principalExpires); errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, false, ErrNotFound
	} else if err != nil {
		return Preview{}, false, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('guest.preview.admission'))`); err != nil {
		return Preview{}, false, err
	}
	var queued int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE kind = $1 AND state IN ('queued', 'running', 'retry_wait')`, workqueue.KindGuestPreview).Scan(&queued); err != nil {
		return Preview{}, false, err
	}
	if queued >= s.queueCapacity {
		return Preview{}, false, ErrCapacity
	}
	var size int64
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(a.verified_size_bytes, 0) FROM media_assets a
		WHERE a.id = $1 AND a.owner_principal_id = $2 AND a.purpose = $3
		  AND a.state = 'ready' AND a.deleted_at IS NULL AND a.attached_at IS NULL
		FOR UPDATE`, input.AudioAssetID, principalID, media.PurposeGuestPreviewAudio).Scan(&size)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, false, ErrNotFound
	}
	if err != nil {
		return Preview{}, false, err
	}
	if size < 1 || size > MaxAudioBytes {
		return Preview{}, false, fmt.Errorf("%w: guest audio is outside the allowed size", media.ErrPayloadTooLarge)
	}
	expiresAt := now.Add(Retention)
	if principalExpires.Before(expiresAt) {
		expiresAt = principalExpires
	}
	previewID, jobID := s.newID(), s.newID()
	_, err = tx.Exec(ctx, `
		INSERT INTO guest_previews
		  (id, guest_principal_id, audio_asset_id, topic, duration, recording_timestamp,
		   practice_type, state, preview_job_id, idempotency_key, request_digest,
		   expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'queued', $8, $9, $10, $11, $12, $12)`,
		previewID, principalID, input.AudioAssetID, input.Topic, input.Duration, timestamp,
		input.PracticeType, jobID, idempotencyKey, requestDigest, expiresAt, now)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return s.resolveCreateRace(ctx, tx, principalID, idempotencyKey, requestDigest)
		}
		return Preview{}, false, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE media_assets SET attached_at = $2, retention_until = $3, updated_at = $2
		WHERE id = $1 AND attached_at IS NULL AND state = 'ready'`, input.AudioAssetID, now, expiresAt)
	if err != nil {
		return Preview{}, false, err
	}
	if result.RowsAffected() != 1 {
		return Preview{}, false, ErrConflict
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: workqueue.KindGuestPreview, ResourceID: previewID, IdempotencyKey: "guest.preview:" + previewID, MaxAttempts: 3}); err != nil {
		return Preview{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Preview{}, false, err
	}
	created, err := s.Find(ctx, principalID, previewID, "")
	return created, true, err
}

func (s *Store) resolveCreateRace(ctx context.Context, tx pgx.Tx, principalID, idempotencyKey, requestDigest string) (Preview, bool, error) {
	_ = tx.Rollback(ctx)
	existing, err := s.Find(ctx, principalID, "", idempotencyKey)
	if err == nil && existing.RequestDigest == requestDigest && existing.ExpiresAt.After(time.Now().UTC()) && existing.State != "promoted" {
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Preview{}, false, err
	}
	return Preview{}, false, ErrConflict
}

func (s *Store) Find(ctx context.Context, principalID, previewID, idempotencyKey string) (Preview, error) {
	query := `
		SELECT id, guest_principal_id, audio_asset_id, topic, duration, recording_timestamp,
		       practice_type, state, transcript, preview_corrections, processing_error,
		       preview_job_id, idempotency_key, request_digest, expires_at, created_at, updated_at
		FROM guest_previews WHERE guest_principal_id = $1`
	args := []any{principalID}
	if strings.TrimSpace(previewID) != "" {
		query += " AND id = $2"
		args = append(args, strings.TrimSpace(previewID))
	} else if strings.TrimSpace(idempotencyKey) != "" {
		query += " AND idempotency_key = $2"
		args = append(args, strings.TrimSpace(idempotencyKey))
	}
	query += " LIMIT 1"
	var preview Preview
	var corrections []byte
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&preview.ID, &preview.GuestPrincipalID, &preview.AudioAssetID, &preview.Topic, &preview.Duration,
		&preview.RecordingTimestamp, &preview.PracticeType, &preview.State, &preview.Transcript,
		&corrections, &preview.ProcessingError, &preview.PreviewJobID, &preview.IdempotencyKey,
		&preview.RequestDigest, &preview.ExpiresAt, &preview.CreatedAt, &preview.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, ErrNotFound
	}
	if err != nil {
		return Preview{}, err
	}
	preview.PreviewCorrections = recording.NormalizeSuggestions(corrections, 2)
	return preview, nil
}

func (s *Store) Expire(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.audio_asset_id, p.preview_job_id FROM guest_previews p
		JOIN media_assets a ON a.id = p.audio_asset_id
		WHERE p.state <> 'promoted' AND p.expires_at <= NOW()
		  AND a.state IN ('ready', 'failed') AND a.deleted_at IS NULL
		ORDER BY p.expires_at ASC FOR UPDATE OF p SKIP LOCKED LIMIT 100`)
	if err != nil {
		return err
	}
	type expiredPreview struct{ id, assetID, jobID string }
	expired := make([]expiredPreview, 0, 100)
	for rows.Next() {
		var item expiredPreview
		if err := rows.Scan(&item.id, &item.assetID, &item.jobID); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, item)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return rowsErr
	}
	for _, item := range expired {
		if _, err := tx.Exec(ctx, `UPDATE guest_previews SET state = 'failed', processing_error = 'Guest preview expired', updated_at = NOW() WHERE id = $1 AND state <> 'promoted' AND expires_at <= NOW()`, item.id); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `UPDATE media_assets SET state = 'deleting', updated_at = NOW() WHERE id = $1 AND state IN ('ready', 'failed') AND deleted_at IS NULL`, item.assetID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_jobs SET state = 'cancelled', completed_at = NOW(), updated_at = NOW() WHERE id = $1 AND state IN ('queued', 'retry_wait')`, item.jobID); err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			continue
		}
		deleteJobID := s.newID()
		if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: deleteJobID, Kind: workqueue.KindMediaDelete, ResourceID: item.assetID, IdempotencyKey: "guest.preview.expire:" + item.assetID + ":" + deleteJobID, MaxAttempts: 20}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) FinalizeFailure(ctx context.Context, tx pgx.Tx, jobID, previewID, message string) error {
	_, err := tx.Exec(ctx, `
		UPDATE guest_previews SET state = 'failed', processing_error = $3, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state IN ('queued', 'processing')`,
		previewID, jobID, truncate(message, 500))
	return err
}

func encodeCorrections(items []recording.Suggestion) string {
	value, _ := json.Marshal(items)
	return string(value)
}
func truncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
