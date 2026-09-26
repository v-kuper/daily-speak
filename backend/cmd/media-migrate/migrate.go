package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"daily-speaking-practice/backend/internal/storage"
)

var migrationExtensionPattern = regexp.MustCompile(`^[a-z0-9]{1,10}$`)

var errMigrationConflict = errors.New("media asset changed while it was being migrated")

type legacyAsset struct {
	ID                     string
	OwnerPrincipalID       string
	Purpose                string
	ObjectKey              string
	ContentType            string
	ExpectedSizeBytes      *int64
	ExpectedChecksumSHA256 *string
}

type switchAssetRequest struct {
	AssetID         string
	SourceObjectKey string
	TargetBucket    string
	TargetObjectKey string
	Size            int64
	SHA256          string
	ETag            string
}

type switchAssetResult struct {
	Updated bool
	Already bool
}

type assetRepository interface {
	ListLegacyLocal(context.Context, int, string) ([]legacyAsset, error)
	SwitchToS3(context.Context, switchAssetRequest) (switchAssetResult, error)
}

type localSource interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

type objectTarget interface {
	Backend() string
	Put(context.Context, storage.PutRequest, io.Reader) (storage.ObjectInfo, error)
	Open(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error)
	Stat(context.Context, string) (storage.ObjectInfo, error)
}

type migrationOptions struct {
	Apply   bool
	Limit   int
	AssetID string
	Bucket  string
}

type migrationSummary struct {
	DryRun         bool
	Scanned        int
	Planned        int
	Copied         int
	Updated        int
	AlreadyApplied int
	Failed         int
	Bytes          int64
}

type migrator struct {
	repository assetRepository
	source     localSource
	target     objectTarget
}

func (migrator migrator) Run(ctx context.Context, options migrationOptions) (migrationSummary, error) {
	summary := migrationSummary{DryRun: !options.Apply}
	if migrator.repository == nil || migrator.source == nil || migrator.target == nil {
		return summary, errors.New("media migration is not configured")
	}
	if migrator.target.Backend() != storage.BackendS3 || strings.TrimSpace(options.Bucket) == "" {
		return summary, errors.New("media migration target must be S3; set MEDIA_STORAGE_DRIVER=s3 and MEDIA_S3_BUCKET")
	}
	if options.Limit < 1 || options.Limit > 10000 {
		return summary, errors.New("migration limit must be between 1 and 10000")
	}
	assets, err := migrator.repository.ListLegacyLocal(ctx, options.Limit, strings.TrimSpace(options.AssetID))
	if err != nil {
		return summary, fmt.Errorf("list legacy local media assets: %w", err)
	}
	var failures []error
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return summary, errors.Join(append(failures, err)...)
		}
		summary.Scanned++
		targetKey, err := migrationTargetKey(asset)
		if err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s: %w", asset.ID, err))
			continue
		}
		size, checksum, err := inspectSource(ctx, migrator.source, asset.ObjectKey)
		if err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s source %q: %w", asset.ID, asset.ObjectKey, err))
			continue
		}
		if err := verifyExpectedMetadata(asset, size, checksum); err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s: %w", asset.ID, err))
			continue
		}
		summary.Planned++
		summary.Bytes += size
		if !options.Apply {
			continue
		}

		source, err := migrator.source.Open(ctx, asset.ObjectKey)
		if err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s reopen source: %w", asset.ID, err))
			continue
		}
		object, putErr := migrator.target.Put(ctx, storage.PutRequest{
			Key: targetKey, ContentType: asset.ContentType, Size: size, SHA256: checksum,
			Metadata: map[string]string{"asset-id": asset.ID, "purpose": asset.Purpose},
		}, source)
		closeErr := source.Close()
		if putErr == nil && closeErr != nil {
			putErr = closeErr
		}
		if putErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s copy to S3: %w", asset.ID, putErr))
			continue
		}
		summary.Copied++
		verified, err := migrator.target.Stat(ctx, targetKey)
		if err != nil || verified.Size != size || !strings.EqualFold(verified.SHA256, checksum) {
			summary.Failed++
			if err == nil {
				if verified.Size != size {
					err = storage.ErrSizeMismatch
				} else {
					err = storage.ErrChecksumMismatch
				}
			}
			failures = append(failures, fmt.Errorf("asset %s verify S3 copy: %w", asset.ID, err))
			continue
		}
		verifiedSize, verifiedChecksum, err := inspectTarget(ctx, migrator.target, targetKey)
		if err != nil || verifiedSize != size || !strings.EqualFold(verifiedChecksum, checksum) {
			summary.Failed++
			if err == nil {
				if verifiedSize != size {
					err = storage.ErrSizeMismatch
				} else {
					err = storage.ErrChecksumMismatch
				}
			}
			failures = append(failures, fmt.Errorf("asset %s verify S3 bytes: %w", asset.ID, err))
			continue
		}
		etag := verified.ETag
		if etag == "" {
			etag = object.ETag
		}
		result, err := migrator.repository.SwitchToS3(ctx, switchAssetRequest{
			AssetID: asset.ID, SourceObjectKey: asset.ObjectKey,
			TargetBucket: options.Bucket, TargetObjectKey: targetKey,
			Size: size, SHA256: checksum, ETag: etag,
		})
		if err != nil {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s publish S3 location: %w", asset.ID, err))
			continue
		}
		if result.Updated {
			summary.Updated++
		} else if result.Already {
			summary.AlreadyApplied++
		} else {
			summary.Failed++
			failures = append(failures, fmt.Errorf("asset %s publish S3 location: %w", asset.ID, errMigrationConflict))
		}
	}
	return summary, errors.Join(failures...)
}

func inspectSource(ctx context.Context, source localSource, key string) (int64, string, error) {
	reader, err := source.Open(ctx, key)
	if err != nil {
		return 0, "", err
	}
	return hashReader(ctx, reader)
}

func inspectTarget(ctx context.Context, target objectTarget, key string) (int64, string, error) {
	reader, _, err := target.Open(ctx, key)
	if err != nil {
		return 0, "", err
	}
	return hashReader(ctx, reader)
}

func hashReader(ctx context.Context, reader io.ReadCloser) (int64, string, error) {
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	size := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			_ = reader.Close()
			return 0, "", err
		}
		read, readErr := reader.Read(buffer)
		if read > 0 {
			written, writeErr := hash.Write(buffer[:read])
			size += int64(written)
			if writeErr != nil || written != read {
				_ = reader.Close()
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				return 0, "", writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = reader.Close()
			return 0, "", readErr
		}
	}
	if err := reader.Close(); err != nil {
		return 0, "", err
	}
	if size <= 0 {
		return 0, "", errors.New("source media file is empty")
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyExpectedMetadata(asset legacyAsset, size int64, checksum string) error {
	if asset.ExpectedSizeBytes != nil && *asset.ExpectedSizeBytes != size {
		return fmt.Errorf("expected size %d but local source has %d: %w", *asset.ExpectedSizeBytes, size, storage.ErrSizeMismatch)
	}
	if asset.ExpectedChecksumSHA256 != nil && !strings.EqualFold(strings.TrimSpace(*asset.ExpectedChecksumSHA256), checksum) {
		return fmt.Errorf("expected checksum %s but local source has %s: %w", *asset.ExpectedChecksumSHA256, checksum, storage.ErrChecksumMismatch)
	}
	return nil
}

func migrationTargetKey(asset legacyAsset) (string, error) {
	assetID := strings.TrimSpace(asset.ID)
	if assetID == "" {
		return "", errors.New("asset ID is empty")
	}
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(asset.ObjectKey), "."))
	if !migrationExtensionPattern.MatchString(extension) {
		extension = extensionForContentType(asset.ContentType)
	}
	hash := sha256.Sum256([]byte(assetID))
	encoded := hex.EncodeToString(hash[:])
	key := path.Join("migrated", encoded[:2], encoded+"."+extension)
	if err := storage.ValidateObjectKey(key); err != nil {
		return "", err
	}
	return key, nil
}

func extensionForContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4", "video/mp4":
		return "m4a"
	case "audio/ogg", "video/ogg":
		return "ogg"
	case "audio/wav", "audio/x-wav", "audio/vnd.wave":
		return "wav"
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "bin"
	}
}
