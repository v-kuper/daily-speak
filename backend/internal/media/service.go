package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/storage"
	"github.com/google/uuid"
)

const (
	defaultUploadTTL          = 24 * time.Hour
	defaultSignedRequestTTL   = 15 * time.Minute
	defaultPartSizeBytes      = 8 * 1024 * 1024
	defaultMaxPartDescriptors = 100
)

var (
	checksumPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

func NewService(repository Repository, store storage.Store, config Config) *Service {
	if config.UploadTTL <= 0 {
		config.UploadTTL = defaultUploadTTL
	}
	if config.SignedRequestTTL <= 0 {
		config.SignedRequestTTL = defaultSignedRequestTTL
	}
	if config.PartSizeBytes <= 0 {
		config.PartSizeBytes = defaultPartSizeBytes
	}
	if config.MaxPartDescriptors <= 0 {
		config.MaxPartDescriptors = defaultMaxPartDescriptors
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{repository: repository, store: store, config: config}
}

func (service *Service) Available() bool {
	return service != nil && service.repository != nil && service.store != nil
}

func (service *Service) CreateUpload(ctx context.Context, input CreateUploadInput) (UploadResource, error) {
	input = normalizeCreateInput(input)
	extension, err := validateCreateInput(input)
	if err != nil || !service.Available() {
		if !service.Available() {
			return UploadResource{}, ErrStorage
		}
		return UploadResource{}, err
	}
	if existing, findErr := service.repository.FindByIdempotency(ctx, input.SessionID, input.IdempotencyKey); findErr == nil {
		if sameUploadRequest(existing.Asset, input) {
			return existing, nil
		}
		return UploadResource{}, ErrConflict
	} else if !errors.Is(findErr, ErrNotFound) {
		return UploadResource{}, findErr
	}
	objectKey, err := storage.NewObjectKey(input.OwnerPrincipalID, input.Purpose, extension)
	if err != nil {
		return UploadResource{}, ErrInvalidRequest
	}
	now := service.config.Now().UTC()
	assetID := uuid.NewString()
	uploadID := uuid.NewString()
	providerUpload, err := service.store.CreateMultipart(ctx, storage.MultipartRequest{
		Key: objectKey, ContentType: input.ContentType, Size: input.SizeBytes,
		SHA256:   input.ChecksumSHA256,
		Metadata: map[string]string{"asset-id": assetID, "owner-principal-id": input.OwnerPrincipalID, "purpose": input.Purpose},
	})
	if err != nil {
		return UploadResource{}, mapStorageError(err)
	}
	partCount := int((input.SizeBytes + service.config.PartSizeBytes - 1) / service.config.PartSizeBytes)
	retentionUntil := now.Add(service.config.UploadTTL)
	resource := UploadResource{
		Asset: Asset{
			ID: assetID, OwnerPrincipalID: input.OwnerPrincipalID, Purpose: input.Purpose,
			State: "uploading", StorageDriver: service.store.Backend(), Bucket: service.config.Bucket,
			ObjectKey: objectKey, ContentType: input.ContentType, ExpectedSizeBytes: input.SizeBytes,
			ExpectedChecksumSHA256: input.ChecksumSHA256, RetentionUntil: &retentionUntil,
			CreatedAt: now, UpdatedAt: now,
		},
		Upload: Upload{
			ID: uploadID, AssetID: assetID, ProviderUploadID: providerUpload.ID,
			State: "uploading", PartSizeBytes: service.config.PartSizeBytes,
			PartCount: partCount, ExpiresAt: retentionUntil,
			CreatedBySessionID: input.SessionID, IdempotencyKey: input.IdempotencyKey,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	if err := service.repository.InsertUpload(ctx, resource.Asset, resource.Upload); err != nil {
		_ = service.store.AbortMultipart(ctx, providerUpload)
		if errors.Is(err, ErrIdempotencyRace) {
			existing, findErr := service.repository.FindByIdempotency(ctx, input.SessionID, input.IdempotencyKey)
			if findErr == nil && sameUploadRequest(existing.Asset, input) {
				return existing, nil
			}
			return UploadResource{}, ErrConflict
		}
		return UploadResource{}, err
	}
	return resource, nil
}

func (service *Service) GetUpload(ctx context.Context, ownerPrincipalID string, uploadID string) (UploadResource, []storage.PartInfo, error) {
	resource, err := service.repository.GetUpload(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(uploadID))
	if err != nil {
		return UploadResource{}, nil, err
	}
	parts, err := service.store.ListParts(ctx, multipartUpload(resource))
	if errors.Is(err, storage.ErrNotFound) && (resource.Upload.State == "completed" || resource.Upload.State == "aborted") {
		return resource, []storage.PartInfo{}, nil
	}
	if err != nil {
		return UploadResource{}, nil, mapStorageError(err)
	}
	return resource, parts, nil
}

func (service *Service) PresignParts(ctx context.Context, ownerPrincipalID string, uploadID string, descriptors []PartDescriptor) ([]SignedPart, error) {
	resource, err := service.repository.GetUpload(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(uploadID))
	if err != nil {
		return nil, err
	}
	if err := service.validateActiveUpload(resource); err != nil {
		return nil, err
	}
	if len(descriptors) == 0 || len(descriptors) > service.config.MaxPartDescriptors {
		return nil, ErrInvalidRequest
	}
	seen := make(map[int]bool, len(descriptors))
	result := make([]SignedPart, 0, len(descriptors))
	for _, descriptor := range descriptors {
		descriptor.ChecksumSHA256 = strings.ToLower(strings.TrimSpace(descriptor.ChecksumSHA256))
		if seen[descriptor.PartNumber] || !service.validPartDescriptor(resource, descriptor) {
			return nil, ErrInvalidRequest
		}
		seen[descriptor.PartNumber] = true
		presigned, signErr := service.store.PresignUploadPart(ctx, multipartUpload(resource), storage.PartRequest{
			Number: int32(descriptor.PartNumber), Size: descriptor.SizeBytes, SHA256: descriptor.ChecksumSHA256,
		}, service.signedTTL(resource.Upload.ExpiresAt))
		if errors.Is(signErr, storage.ErrUnsupported) && service.store.Backend() == storage.BackendLocal {
			result = append(result, SignedPart{
				Descriptor: descriptor,
				Request: storage.PresignedRequest{
					Method:    http.MethodPut,
					Headers:   http.Header{"Content-Type": []string{resource.Asset.ContentType}},
					ExpiresAt: service.config.Now().Add(service.signedTTL(resource.Upload.ExpiresAt)).UTC(),
				},
				Local: true,
			})
			continue
		}
		if signErr != nil {
			return nil, mapStorageError(signErr)
		}
		result = append(result, SignedPart{Descriptor: descriptor, Request: presigned})
	}
	return result, nil
}

func (service *Service) PutLocalPart(ctx context.Context, uploadID string, descriptor PartDescriptor, bodySize int64, body io.Reader) (storage.PartInfo, error) {
	if service.store == nil || service.store.Backend() != storage.BackendLocal || body == nil || bodySize != descriptor.SizeBytes {
		return storage.PartInfo{}, ErrInvalidRequest
	}
	resource, err := service.repository.GetUploadByID(ctx, strings.TrimSpace(uploadID))
	if err != nil {
		return storage.PartInfo{}, err
	}
	if err := service.validateActiveUpload(resource); err != nil || !service.validPartDescriptor(resource, descriptor) {
		if err != nil {
			return storage.PartInfo{}, err
		}
		return storage.PartInfo{}, ErrInvalidRequest
	}
	part, err := service.store.PutPart(ctx, multipartUpload(resource), storage.PartRequest{
		Number: int32(descriptor.PartNumber), Size: descriptor.SizeBytes,
		SHA256: strings.ToLower(strings.TrimSpace(descriptor.ChecksumSHA256)),
	}, body)
	if err != nil {
		return storage.PartInfo{}, mapStorageError(err)
	}
	now := service.config.Now().UTC()
	if err := service.repository.UpsertPart(ctx, Part{
		UploadID: uploadID, PartNumber: descriptor.PartNumber, SizeBytes: part.Size,
		ETag: part.ETag, ChecksumSHA256: part.SHA256, VerifiedAt: &now,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return storage.PartInfo{}, err
	}
	return part, nil
}

func (service *Service) CompleteUpload(ctx context.Context, ownerPrincipalID string, uploadID string, completed []CompletedPart) (UploadResource, error) {
	resource, err := service.repository.GetUpload(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(uploadID))
	if err != nil {
		return UploadResource{}, err
	}
	if resource.Upload.State == "completed" && resource.Asset.State == "ready" {
		return resource, nil
	}
	if len(completed) != resource.Upload.PartCount {
		return UploadResource{}, ErrInvalidRequest
	}
	parts := make([]storage.CompletedPart, 0, len(completed))
	seen := make(map[int]bool, len(completed))
	for _, item := range completed {
		item.ETag = strings.TrimSpace(item.ETag)
		item.ChecksumSHA256 = strings.ToLower(strings.TrimSpace(item.ChecksumSHA256))
		if item.PartNumber < 1 || item.PartNumber > resource.Upload.PartCount || seen[item.PartNumber] || item.ETag == "" || (item.ChecksumSHA256 != "" && !checksumPattern.MatchString(item.ChecksumSHA256)) {
			return UploadResource{}, ErrInvalidRequest
		}
		seen[item.PartNumber] = true
		parts = append(parts, storage.CompletedPart{Number: int32(item.PartNumber), ETag: item.ETag, SHA256: item.ChecksumSHA256})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	switch resource.Upload.State {
	case "pending", "uploading":
		if !resource.Upload.ExpiresAt.After(service.config.Now()) {
			return UploadResource{}, ErrExpired
		}
		if err := service.repository.ClaimCompleting(ctx, uploadID); err != nil {
			return UploadResource{}, err
		}
		resource.Upload.State = "completing"
	case "completing":
		// A retry may finish an object that the provider accepted before the
		// previous request lost its response. Storage completion is idempotent.
	default:
		return UploadResource{}, ErrConflict
	}
	info, err := service.store.CompleteMultipart(ctx, multipartUpload(resource), parts)
	if err != nil {
		return UploadResource{}, mapStorageError(err)
	}
	if info.Size != resource.Asset.ExpectedSizeBytes || !strings.EqualFold(info.SHA256, resource.Asset.ExpectedChecksumSHA256) {
		return UploadResource{}, ErrInvalidRequest
	}
	completedResource, err := service.repository.MarkCompleted(ctx, uploadID, info, service.config.Now().UTC())
	if err == nil {
		return completedResource, nil
	}
	// Never remove a concurrently published object. If the database confirms
	// that another completion won, return it. Deletion is safe only after an
	// abort transition fenced all future MarkCompleted calls; transient DB
	// failures and an ordinary `completing` snapshot are left for retry/sweep.
	current, currentErr := service.repository.GetUploadByID(ctx, uploadID)
	if currentErr == nil && current.Upload.State == "completed" && current.Asset.State == "ready" {
		return current, nil
	}
	if currentErr == nil && (current.Upload.State == "aborting" || current.Upload.State == "aborted" || current.Upload.State == "expired") {
		_ = service.store.Delete(ctx, resource.Asset.ObjectKey)
	}
	return UploadResource{}, err
}

func (service *Service) AbortUpload(ctx context.Context, ownerPrincipalID string, uploadID string) (UploadResource, error) {
	return service.abortUpload(ctx, ownerPrincipalID, uploadID, nil)
}

func (service *Service) AbortExpiredUpload(ctx context.Context, ownerPrincipalID string, uploadID string, staleCompletingAfter time.Duration) (UploadResource, error) {
	if staleCompletingAfter <= 0 {
		staleCompletingAfter = 30 * time.Minute
	}
	staleBefore := service.config.Now().Add(-staleCompletingAfter).UTC()
	return service.abortUpload(ctx, ownerPrincipalID, uploadID, &staleBefore)
}

func (service *Service) abortUpload(ctx context.Context, ownerPrincipalID string, uploadID string, staleCompletingBefore *time.Time) (UploadResource, error) {
	resource, err := service.repository.GetUpload(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(uploadID))
	if err != nil {
		return UploadResource{}, err
	}
	if resource.Upload.State == "aborted" {
		return resource, nil
	}
	if resource.Upload.State == "completed" || resource.Asset.State == "ready" {
		return UploadResource{}, ErrConflict
	}
	if resource.Upload.State != "aborting" {
		if err := service.repository.ClaimAborting(ctx, uploadID, staleCompletingBefore); err != nil {
			return UploadResource{}, err
		}
		resource.Upload.State = "aborting"
	}
	if err := service.store.AbortMultipart(ctx, multipartUpload(resource)); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return UploadResource{}, mapStorageError(err)
	}
	return service.repository.MarkAborted(ctx, uploadID, service.config.Now().UTC())
}

func (service *Service) Download(ctx context.Context, ownerPrincipalID string, assetID string) (Download, error) {
	asset, err := service.repository.GetReadyAsset(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(assetID))
	if err != nil {
		return Download{}, err
	}
	presigned, signErr := service.store.PresignGet(ctx, asset.ObjectKey, service.config.SignedRequestTTL)
	if errors.Is(signErr, storage.ErrUnsupported) && service.store.Backend() == storage.BackendLocal {
		return Download{Asset: asset, Request: storage.PresignedRequest{
			Method: http.MethodGet, Headers: http.Header{},
			ExpiresAt: service.config.Now().Add(service.config.SignedRequestTTL).UTC(),
		}, Local: true}, nil
	}
	if signErr != nil {
		return Download{}, mapStorageError(signErr)
	}
	return Download{Asset: asset, Request: presigned}, nil
}

func (service *Service) OpenSignedContent(ctx context.Context, assetID string) (Content, error) {
	asset, err := service.repository.GetReadyAssetByID(ctx, strings.TrimSpace(assetID))
	if err != nil {
		return Content{}, err
	}
	body, info, err := service.store.Open(ctx, asset.ObjectKey)
	if err != nil {
		return Content{}, mapStorageError(err)
	}
	if (asset.ExpectedSizeBytes > 0 && info.Size != asset.ExpectedSizeBytes) ||
		(asset.ExpectedChecksumSHA256 != "" && !strings.EqualFold(info.SHA256, asset.ExpectedChecksumSHA256)) {
		_ = body.Close()
		return Content{}, ErrNotFound
	}
	return Content{Body: body, Info: info}, nil
}

func normalizeCreateInput(input CreateUploadInput) CreateUploadInput {
	input.OwnerPrincipalID = strings.TrimSpace(input.OwnerPrincipalID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Purpose = strings.ToLower(strings.TrimSpace(input.Purpose))
	input.ContentType = strings.ToLower(strings.TrimSpace(strings.Split(input.ContentType, ";")[0]))
	input.ChecksumSHA256 = strings.ToLower(strings.TrimSpace(input.ChecksumSHA256))
	return input
}

func validateCreateInput(input CreateUploadInput) (string, error) {
	if input.OwnerPrincipalID == "" || input.SessionID == "" || !idempotencyPattern.MatchString(input.IdempotencyKey) || input.SizeBytes <= 0 {
		return "", ErrInvalidRequest
	}
	if !checksumPattern.MatchString(input.ChecksumSHA256) {
		return "", ErrChecksumMismatch
	}
	switch input.Purpose {
	case PurposeRecordingAudio:
		allowed := map[string]bool{
			"audio/webm": true, "video/webm": true, "audio/mp4": true,
			"audio/x-m4a": true, "video/mp4": true, "audio/ogg": true,
			"video/ogg": true, "audio/wav": true, "audio/x-wav": true,
			"audio/vnd.wave": true, "audio/mpeg": true,
		}
		if !allowed[input.ContentType] {
			return "", ErrUnsupportedType
		}
		extension := domain.ResolveAudioExtension(input.ContentType)
		if input.SizeBytes > domain.MaxAudioUploadBytes {
			return "", ErrPayloadTooLarge
		}
		return extension, nil
	case PurposeRecordingPhoto:
		extensions := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp", "image/gif": "gif"}
		extension := extensions[input.ContentType]
		if extension == "" {
			return "", ErrUnsupportedType
		}
		if input.SizeBytes > domain.MaxPhotoUploadBytes {
			return "", ErrPayloadTooLarge
		}
		return extension, nil
	default:
		return "", ErrInvalidRequest
	}
}

func sameUploadRequest(asset Asset, input CreateUploadInput) bool {
	return asset.OwnerPrincipalID == input.OwnerPrincipalID && asset.Purpose == input.Purpose &&
		asset.ContentType == input.ContentType && asset.ExpectedSizeBytes == input.SizeBytes &&
		strings.EqualFold(asset.ExpectedChecksumSHA256, input.ChecksumSHA256)
}

func multipartUpload(resource UploadResource) storage.MultipartUpload {
	return storage.MultipartUpload{
		ID: resource.Upload.ProviderUploadID, Key: resource.Asset.ObjectKey,
		ContentType: resource.Asset.ContentType, Size: resource.Asset.ExpectedSizeBytes,
		SHA256:   resource.Asset.ExpectedChecksumSHA256,
		Metadata: map[string]string{"asset-id": resource.Asset.ID, "owner-principal-id": resource.Asset.OwnerPrincipalID, "purpose": resource.Asset.Purpose},
	}
}

func (service *Service) validateActiveUpload(resource UploadResource) error {
	if resource.Upload.State != "pending" && resource.Upload.State != "uploading" {
		return ErrConflict
	}
	if !resource.Upload.ExpiresAt.After(service.config.Now()) {
		return ErrExpired
	}
	return nil
}

func (service *Service) validPartDescriptor(resource UploadResource, descriptor PartDescriptor) bool {
	checksum := strings.ToLower(strings.TrimSpace(descriptor.ChecksumSHA256))
	if descriptor.PartNumber < 1 || descriptor.PartNumber > resource.Upload.PartCount || !checksumPattern.MatchString(checksum) {
		return false
	}
	expectedSize := resource.Upload.PartSizeBytes
	if descriptor.PartNumber == resource.Upload.PartCount {
		expectedSize = resource.Asset.ExpectedSizeBytes - int64(resource.Upload.PartCount-1)*resource.Upload.PartSizeBytes
	}
	return descriptor.SizeBytes == expectedSize
}

func (service *Service) signedTTL(uploadExpiresAt time.Time) time.Duration {
	ttl := service.config.SignedRequestTTL
	if remaining := uploadExpiresAt.Sub(service.config.Now()); remaining < ttl {
		ttl = remaining
	}
	return ttl
}

func mapStorageError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, storage.ErrConflict):
		return ErrConflict
	case errors.Is(err, storage.ErrChecksumMismatch):
		return ErrChecksumMismatch
	case errors.Is(err, storage.ErrSizeMismatch):
		return ErrSizeMismatch
	case errors.Is(err, storage.ErrInvalidKey), errors.Is(err, storage.ErrInvalidRequest):
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	default:
		return fmt.Errorf("%w: %v", ErrStorage, err)
	}
}
