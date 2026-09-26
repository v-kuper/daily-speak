package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/storage"
)

type Materializer struct {
	db    *db.DB
	store storage.Store
}

func NewMaterializer(database *db.DB, store storage.Store) *Materializer {
	return &Materializer{db: database, store: store}
}

// Materialize gives path-based processors such as whisper.cpp a
// bounded temporary file while keeping the durable source in the configured
// store. Local and S3 objects therefore follow the same integrity path and a
// worker never depends on an API container's filesystem.
func (m *Materializer) Materialize(ctx context.Context, assetID string) (string, func(), error) {
	if m == nil || m.db == nil || m.store == nil {
		return "", func() {}, errors.New("media storage is not configured")
	}
	var driver, objectKey, contentType, expectedChecksum string
	var expectedSize int64
	err := m.db.QueryRow(ctx, `
		SELECT storage_driver, object_key, content_type,
		       COALESCE(verified_size_bytes, expected_size_bytes, 0),
		       COALESCE(verified_checksum_sha256, expected_checksum_sha256, '')
		FROM media_assets
		WHERE id = $1 AND state = 'ready' AND deleted_at IS NULL
		LIMIT 1`, strings.TrimSpace(assetID)).Scan(
		&driver, &objectKey, &contentType, &expectedSize, &expectedChecksum,
	)
	if err != nil {
		return "", func() {}, err
	}
	if driver != m.store.Backend() {
		return "", func() {}, fmt.Errorf("media asset requires %s storage, worker has %s", driver, m.store.Backend())
	}
	body, info, err := m.store.Open(ctx, objectKey)
	if err != nil {
		return "", func() {}, err
	}
	defer body.Close()
	extension := domain.ResolveAudioExtension(contentType)
	if extension == "" {
		extension = "bin"
	}
	temporary, err := os.CreateTemp("", "daily-speaking-recording-*."+extension)
	if err != nil {
		return "", func() {}, err
	}
	path := temporary.Name()
	cleanup := func() { _ = os.Remove(path) }
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(body, domain.MaxAudioUploadBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		cleanup()
		return "", func() {}, copyErr
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, closeErr
	}
	if written <= 0 || written > domain.MaxAudioUploadBytes {
		cleanup()
		return "", func() {}, errors.New("media asset size is outside recording limits")
	}
	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	if expectedSize > 0 && written != expectedSize {
		cleanup()
		return "", func() {}, errors.New("media asset size verification failed")
	}
	if expectedChecksum != "" && !strings.EqualFold(actualChecksum, expectedChecksum) {
		cleanup()
		return "", func() {}, errors.New("media asset checksum verification failed")
	}
	if info.Size > 0 && info.Size != written {
		cleanup()
		return "", func() {}, errors.New("storage metadata size verification failed")
	}
	if info.SHA256 != "" && !strings.EqualFold(info.SHA256, actualChecksum) {
		cleanup()
		return "", func() {}, errors.New("storage metadata checksum verification failed")
	}
	return path, cleanup, nil
}
