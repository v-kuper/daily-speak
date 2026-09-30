package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GeneratedStore is the storage adapter for worker-created private artifacts.
// Objects are registered before publishing their feature reference. Pending
// registrations expire so crashes between upload and feature commit cannot leak
// objects indefinitely.
type GeneratedStore struct {
	store      storage.Store
	bucket     string
	repository *SQLRepository
}

func NewGeneratedStore(store storage.Store, bucket string, repository *SQLRepository) *GeneratedStore {
	if store != nil && store.Backend() == storage.BackendLocal {
		bucket = ""
	}
	return &GeneratedStore{store: store, bucket: bucket, repository: repository}
}

func (s *GeneratedStore) Find(ctx context.Context, key string) (*Asset, error) {
	if s == nil || s.store == nil {
		return nil, ErrStorage
	}
	info, err := s.store.Stat(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, mapStorageError(err)
	}
	asset := assetFromObject(s.store.Backend(), s.bucket, info)
	if asset.ID == "" || asset.OwnerPrincipalID == "" || asset.Purpose == "" {
		return nil, ErrConflict
	}
	if s.repository != nil {
		if err := s.repository.RegisterGenerated(ctx, asset); err != nil {
			return nil, err
		}
	}
	return &asset, nil
}

func (s *GeneratedStore) Write(ctx context.Context, owner, purpose, key, contentType string, data []byte) (Asset, error) {
	if s == nil || s.store == nil {
		return Asset{}, ErrStorage
	}
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	assetID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(s.store.Backend()+":"+s.bucket+":"+key)).String()
	if s.repository != nil {
		if err := s.repository.ReserveGenerated(ctx, Asset{ID: assetID, OwnerPrincipalID: owner, Purpose: purpose, StorageDriver: s.store.Backend(), Bucket: s.bucket, ObjectKey: key, ContentType: contentType, ExpectedSizeBytes: int64(len(data)), ExpectedChecksumSHA256: checksum}); err != nil {
			return Asset{}, err
		}
	}
	info, err := s.store.Put(ctx, storage.PutRequest{Key: key, ContentType: contentType, Size: int64(len(data)), SHA256: checksum,
		Metadata: map[string]string{"asset-id": assetID, "owner-principal-id": owner, "purpose": purpose}}, bytes.NewReader(data))
	if err != nil {
		return Asset{}, mapStorageError(err)
	}
	asset := assetFromObject(s.store.Backend(), s.bucket, info)
	// Some adapters return only verified object fields from Put.
	if asset.ID == "" {
		asset.ID = assetID
	}
	if asset.OwnerPrincipalID == "" {
		asset.OwnerPrincipalID = owner
	}
	if asset.Purpose == "" {
		asset.Purpose = purpose
	}
	if asset.ObjectKey == "" {
		asset.ObjectKey = key
	}
	if asset.ContentType == "" {
		asset.ContentType = contentType
	}
	if s.repository != nil {
		if err := s.repository.RegisterGenerated(ctx, asset); err != nil {
			return Asset{}, err
		}
	}
	return asset, nil
}

func assetFromObject(driver, bucket string, info storage.ObjectInfo) Asset {
	size, checksum, etag := info.Size, info.SHA256, info.ETag
	return Asset{ID: info.Metadata["asset-id"], OwnerPrincipalID: info.Metadata["owner-principal-id"], Purpose: info.Metadata["purpose"], State: "ready", StorageDriver: driver, Bucket: bucket, ObjectKey: info.Key, ContentType: info.ContentType, ExpectedSizeBytes: size, VerifiedSizeBytes: &size, ExpectedChecksumSHA256: checksum, VerifiedChecksumSHA256: &checksum, ETag: &etag}
}

func (r *SQLRepository) RegisterGenerated(ctx context.Context, asset Asset) error {
	result, err := r.database.Exec(ctx, `INSERT INTO media_assets
 (id,owner_principal_id,purpose,state,storage_driver,bucket,object_key,content_type,expected_size_bytes,verified_size_bytes,
 expected_checksum_sha256,verified_checksum_sha256,etag,verified_at,retention_until)
 VALUES($1,$2,$3,'ready',$4,NULLIF($5,''),$6,$7,$8,$8,$9,$9,NULLIF($10,''),NOW(),NOW()+INTERVAL '24 hours')
 ON CONFLICT(id) DO UPDATE SET state='ready',expected_size_bytes=EXCLUDED.expected_size_bytes,verified_size_bytes=EXCLUDED.verified_size_bytes,
 expected_checksum_sha256=EXCLUDED.expected_checksum_sha256,verified_checksum_sha256=EXCLUDED.verified_checksum_sha256,etag=EXCLUDED.etag,verified_at=NOW(),updated_at=NOW()
 WHERE media_assets.state IN ('failed','ready') AND media_assets.object_key=EXCLUDED.object_key
 AND (media_assets.state='failed' OR media_assets.verified_checksum_sha256=EXCLUDED.verified_checksum_sha256)`, asset.ID, asset.OwnerPrincipalID, asset.Purpose, asset.StorageDriver, asset.Bucket, asset.ObjectKey, asset.ContentType, asset.ExpectedSizeBytes, asset.ExpectedChecksumSHA256, stringPointerValue(asset.ETag))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

// AttachGenerated must be called by the feature repository in its publication
// transaction, after checking the feature's current lease and owner.
func AttachGenerated(ctx context.Context, tx pgx.Tx, asset Asset) error {
	result, err := tx.Exec(ctx, `UPDATE media_assets SET attached_at=NOW(),retention_until=NULL,updated_at=NOW()
 WHERE id=$1 AND owner_principal_id=$2 AND state='ready' AND deleted_at IS NULL`, asset.ID, asset.OwnerPrincipalID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// QuestionDownload allows only the owning guest to read a worker-generated
// question artifact. Uploaded learner audio remains account-only.
func (service *Service) QuestionDownload(ctx context.Context, input DownloadInput) (Download, error) {
	if !service.Available() {
		return Download{}, ErrStorage
	}
	asset, err := service.repository.GetReadyAsset(ctx, input.OwnerPrincipalID, input.AssetID)
	if err != nil {
		return Download{}, err
	}
	if asset.Purpose != PurposeInterviewQuestionAudio {
		return Download{}, ErrGuestRestricted
	}
	return service.signDownload(ctx, asset)
}

func (service *Service) signDownload(ctx context.Context, asset Asset) (Download, error) {
	presigned, err := service.store.PresignGet(ctx, asset.ObjectKey, service.config.SignedRequestTTL)
	if errors.Is(err, storage.ErrUnsupported) && service.store.Backend() == storage.BackendLocal {
		return Download{Asset: asset, Request: SignedRequest{Method: "GET", Headers: map[string][]string{}, ExpiresAt: service.config.Now().Add(service.config.SignedRequestTTL).UTC()}, Local: true}, nil
	}
	if err != nil {
		return Download{}, mapStorageError(err)
	}
	return Download{Asset: asset, Request: signedRequestFromStorage(presigned)}, nil
}

// RetireArtifacts only marks objects. The bounded cleanup sweep enqueues their
// physical removal later, using its existing retry and concurrency controls.
func RetireArtifacts(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE media_assets SET attached_at=NULL,retention_until=$2,updated_at=NOW()
 WHERE id=ANY($1::text[]) AND state NOT IN ('deleting','deleted')`, ids, time.Now().UTC())
	return err
}

// Reserve before Put: even a crash after upload leaves a durable cleanup key.
func (r *SQLRepository) ReserveGenerated(ctx context.Context, asset Asset) error {
	result, err := r.database.Exec(ctx, `INSERT INTO media_assets(id,owner_principal_id,purpose,state,storage_driver,bucket,object_key,content_type,expected_size_bytes,expected_checksum_sha256,retention_until)
 VALUES($1,$2,$3,'failed',$4,NULLIF($5,''),$6,$7,$8,$9,NOW()+INTERVAL '24 hours')
 ON CONFLICT(id) DO UPDATE SET expected_size_bytes=EXCLUDED.expected_size_bytes,expected_checksum_sha256=EXCLUDED.expected_checksum_sha256,updated_at=NOW()
 WHERE media_assets.state='failed' AND media_assets.attached_at IS NULL AND media_assets.object_key=EXCLUDED.object_key`, asset.ID, asset.OwnerPrincipalID, asset.Purpose, asset.StorageDriver, asset.Bucket, asset.ObjectKey, asset.ContentType, asset.ExpectedSizeBytes, asset.ExpectedChecksumSHA256)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
