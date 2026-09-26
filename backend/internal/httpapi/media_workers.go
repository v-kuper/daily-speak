package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

// materializeMediaAsset gives path-based processors such as whisper.cpp a
// bounded temporary file while keeping the durable source in the configured
// store. Local and S3 objects therefore follow the same integrity path and a
// worker never depends on an API container's filesystem.
func (s *Server) materializeMediaAsset(ctx context.Context, assetID string) (string, func(), error) {
	if s.mediaStore == nil {
		return "", func() {}, errors.New("media storage is not configured")
	}
	var driver, objectKey, contentType, expectedChecksum string
	var expectedSize int64
	err := s.db.QueryRow(ctx, `
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
	if driver != s.mediaStore.Backend() {
		return "", func() {}, fmt.Errorf("media asset requires %s storage, worker has %s", driver, s.mediaStore.Backend())
	}
	body, info, err := s.mediaStore.Open(ctx, objectKey)
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
