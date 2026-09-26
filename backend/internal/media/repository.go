package media

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type SQLRepository struct {
	database *db.DB
}

func NewSQLRepository(database *db.DB) *SQLRepository {
	return &SQLRepository{database: database}
}

func (repository *SQLRepository) FindByIdempotency(ctx context.Context, sessionID string, key string) (UploadResource, error) {
	return repository.queryUpload(ctx, `
		SELECT `+uploadColumns+`
		FROM media_uploads u
		JOIN media_assets a ON a.id = u.asset_id
		WHERE u.created_by_session_id = $1 AND u.idempotency_key = $2
		LIMIT 1`, sessionID, key)
}

func (repository *SQLRepository) InsertUpload(ctx context.Context, asset Asset, upload Upload) error {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var bucket any
	if asset.Bucket != "" {
		bucket = asset.Bucket
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, bucket, object_key,
		   content_type, expected_size_bytes, expected_checksum_sha256, retention_until,
		   created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)`,
		asset.ID, asset.OwnerPrincipalID, asset.Purpose, asset.State, asset.StorageDriver,
		bucket, asset.ObjectKey, asset.ContentType, asset.ExpectedSizeBytes,
		asset.ExpectedChecksumSHA256, asset.RetentionUntil, asset.CreatedAt)
	if err != nil {
		return mapRepositoryError(err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO media_uploads
		  (id, asset_id, provider_upload_id, state, part_size_bytes, part_count,
		   expires_at, created_by_session_id, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
		upload.ID, upload.AssetID, upload.ProviderUploadID, upload.State,
		upload.PartSizeBytes, upload.PartCount, upload.ExpiresAt,
		upload.CreatedBySessionID, upload.IdempotencyKey, upload.CreatedAt)
	if err != nil {
		return mapRepositoryError(err)
	}
	return tx.Commit(ctx)
}

func (repository *SQLRepository) GetUpload(ctx context.Context, ownerPrincipalID string, uploadID string) (UploadResource, error) {
	return repository.queryUpload(ctx, `
		SELECT `+uploadColumns+`
		FROM media_uploads u
		JOIN media_assets a ON a.id = u.asset_id
		WHERE u.id = $1 AND a.owner_principal_id = $2
		LIMIT 1`, uploadID, ownerPrincipalID)
}

func (repository *SQLRepository) GetUploadByID(ctx context.Context, uploadID string) (UploadResource, error) {
	return repository.queryUpload(ctx, `
		SELECT `+uploadColumns+`
		FROM media_uploads u
		JOIN media_assets a ON a.id = u.asset_id
		WHERE u.id = $1
		LIMIT 1`, uploadID)
}

func (repository *SQLRepository) UpsertPart(ctx context.Context, part Part) error {
	_, err := repository.database.Exec(ctx, `
		INSERT INTO media_upload_parts
		  (upload_id, part_number, size_bytes, etag, checksum_sha256, verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $7)
		ON CONFLICT (upload_id, part_number) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes,
		    etag = EXCLUDED.etag,
		    checksum_sha256 = EXCLUDED.checksum_sha256,
		    verified_at = EXCLUDED.verified_at,
		    updated_at = EXCLUDED.updated_at`,
		part.UploadID, part.PartNumber, part.SizeBytes, part.ETag,
		part.ChecksumSHA256, part.VerifiedAt, part.CreatedAt)
	return err
}

func (repository *SQLRepository) ClaimCompleting(ctx context.Context, uploadID string) error {
	result, err := repository.database.Exec(ctx, `
		UPDATE media_uploads
		SET state = 'completing', updated_at = NOW()
		WHERE id = $1 AND state IN ('pending', 'uploading')`, uploadID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (repository *SQLRepository) ClaimAborting(ctx context.Context, uploadID string, staleCompletingBefore *time.Time) error {
	result, err := repository.database.Exec(ctx, `
		UPDATE media_uploads
		SET state = 'aborting', updated_at = NOW()
		WHERE id = $1
		  AND (
		    state IN ('pending', 'uploading', 'failed')
		    OR ($2::timestamptz IS NOT NULL AND state = 'completing' AND updated_at <= $2)
		  )`, uploadID, staleCompletingBefore)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (repository *SQLRepository) MarkCompleted(ctx context.Context, uploadID string, info storage.ObjectInfo, now time.Time) (UploadResource, error) {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return UploadResource{}, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE media_uploads
		SET state = 'completed', completed_at = COALESCE(completed_at, $2), updated_at = $2
		WHERE id = $1 AND state IN ('completing', 'completed')`, uploadID, now)
	if err != nil {
		return UploadResource{}, err
	}
	if result.RowsAffected() == 0 {
		return UploadResource{}, ErrConflict
	}
	result, err = tx.Exec(ctx, `
		UPDATE media_assets a
		SET state = 'ready', verified_size_bytes = $2,
		    verified_checksum_sha256 = $3, etag = NULLIF($4, ''),
		    verified_at = COALESCE(verified_at, $5), updated_at = $5
		FROM media_uploads u
		WHERE u.id = $1 AND a.id = u.asset_id
		  AND a.state IN ('pending', 'uploading', 'uploaded', 'verifying', 'ready')`,
		uploadID, info.Size, info.SHA256, info.ETag, now)
	if err != nil {
		return UploadResource{}, err
	}
	if result.RowsAffected() == 0 {
		return UploadResource{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return UploadResource{}, err
	}
	return repository.GetUploadByID(ctx, uploadID)
}

func (repository *SQLRepository) MarkAborted(ctx context.Context, uploadID string, now time.Time) (UploadResource, error) {
	tx, err := repository.database.Begin(ctx)
	if err != nil {
		return UploadResource{}, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE media_uploads
		SET state = 'aborted', aborted_at = COALESCE(aborted_at, $2), updated_at = $2
		WHERE id = $1 AND state IN ('aborting', 'aborted')`, uploadID, now)
	if err != nil {
		return UploadResource{}, err
	}
	if result.RowsAffected() == 0 {
		return UploadResource{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `
		UPDATE media_assets a
		SET state = 'failed', updated_at = $2
		FROM media_uploads u
		WHERE u.id = $1 AND a.id = u.asset_id AND a.state <> 'ready'`, uploadID, now)
	if err != nil {
		return UploadResource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UploadResource{}, err
	}
	return repository.GetUploadByID(ctx, uploadID)
}

func (repository *SQLRepository) GetReadyAsset(ctx context.Context, ownerPrincipalID string, assetID string) (Asset, error) {
	return repository.queryAsset(ctx, `
		SELECT `+assetColumns+`
		FROM media_assets a
		WHERE a.id = $1 AND a.owner_principal_id = $2
		  AND a.state = 'ready' AND a.deleted_at IS NULL
		LIMIT 1`, assetID, ownerPrincipalID)
}

func (repository *SQLRepository) GetReadyAssetByID(ctx context.Context, assetID string) (Asset, error) {
	return repository.queryAsset(ctx, `
		SELECT `+assetColumns+`
		FROM media_assets a
		WHERE a.id = $1 AND a.state = 'ready' AND a.deleted_at IS NULL
		LIMIT 1`, assetID)
}

const assetColumns = `
	a.id, a.owner_principal_id, a.purpose, a.state, a.storage_driver,
	COALESCE(a.bucket, ''), a.object_key, a.content_type,
	COALESCE(a.expected_size_bytes, a.verified_size_bytes, 0),
	a.verified_size_bytes,
	COALESCE(a.expected_checksum_sha256, a.verified_checksum_sha256, ''),
	a.verified_checksum_sha256, a.etag, a.retention_until, a.verified_at,
	a.attached_at, a.deleted_at, a.created_at, a.updated_at`

const uploadColumns = assetColumns + `,
	u.id, u.asset_id, COALESCE(u.provider_upload_id, ''), u.state,
	u.part_size_bytes, u.part_count, u.expires_at,
	COALESCE(u.created_by_session_id, ''), u.idempotency_key,
	u.completed_at, u.aborted_at, u.created_at, u.updated_at`

type rowScanner interface {
	Scan(...any) error
}

func scanAsset(row rowScanner) (Asset, error) {
	var asset Asset
	err := row.Scan(
		&asset.ID, &asset.OwnerPrincipalID, &asset.Purpose, &asset.State,
		&asset.StorageDriver, &asset.Bucket, &asset.ObjectKey, &asset.ContentType,
		&asset.ExpectedSizeBytes, &asset.VerifiedSizeBytes,
		&asset.ExpectedChecksumSHA256, &asset.VerifiedChecksumSHA256, &asset.ETag,
		&asset.RetentionUntil, &asset.VerifiedAt, &asset.AttachedAt, &asset.DeletedAt,
		&asset.CreatedAt, &asset.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return asset, err
}

func (repository *SQLRepository) queryAsset(ctx context.Context, query string, args ...any) (Asset, error) {
	return scanAsset(repository.database.QueryRow(ctx, query, args...))
}

func (repository *SQLRepository) queryUpload(ctx context.Context, query string, args ...any) (UploadResource, error) {
	var resource UploadResource
	err := repository.database.QueryRow(ctx, query, args...).Scan(
		&resource.Asset.ID, &resource.Asset.OwnerPrincipalID, &resource.Asset.Purpose,
		&resource.Asset.State, &resource.Asset.StorageDriver, &resource.Asset.Bucket,
		&resource.Asset.ObjectKey, &resource.Asset.ContentType,
		&resource.Asset.ExpectedSizeBytes, &resource.Asset.VerifiedSizeBytes,
		&resource.Asset.ExpectedChecksumSHA256, &resource.Asset.VerifiedChecksumSHA256,
		&resource.Asset.ETag, &resource.Asset.RetentionUntil, &resource.Asset.VerifiedAt,
		&resource.Asset.AttachedAt, &resource.Asset.DeletedAt, &resource.Asset.CreatedAt,
		&resource.Asset.UpdatedAt, &resource.Upload.ID, &resource.Upload.AssetID,
		&resource.Upload.ProviderUploadID, &resource.Upload.State,
		&resource.Upload.PartSizeBytes, &resource.Upload.PartCount,
		&resource.Upload.ExpiresAt, &resource.Upload.CreatedBySessionID,
		&resource.Upload.IdempotencyKey, &resource.Upload.CompletedAt,
		&resource.Upload.AbortedAt, &resource.Upload.CreatedAt, &resource.Upload.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return UploadResource{}, ErrNotFound
	}
	return resource, err
}

func mapRepositoryError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrIdempotencyRace
	}
	return err
}
