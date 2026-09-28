package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/storage"
	"github.com/google/uuid"
)

const (
	defaultUploadTTL          = 24 * time.Hour
	defaultSignedRequestTTL   = 15 * time.Minute
	defaultPartSizeBytes      = 8 * 1024 * 1024
	defaultMaxPartDescriptors = 100
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
	input, err := applyCreateOwnerPolicy(input)
	if err != nil {
		return UploadResource{}, err
	}
	extension, err := validateCreateInput(input)
	if err != nil || !service.Available() {
		if !service.Available() {
			return UploadResource{}, ErrStorage
		}
		return UploadResource{}, err
	}
	if existing, findErr := service.repository.FindByIdempotency(ctx, input.SessionID, input.IdempotencyKey); findErr == nil {
		if sameUploadRequest(existing.Asset, input) && existing.Upload.InterviewSessionID == input.InterviewSessionID {
			return existing, nil
		}
		return UploadResource{}, ErrConflict
	} else if !errors.Is(findErr, ErrNotFound) {
		return UploadResource{}, findErr
	}
	if input.Purpose == PurposeGuestPreviewAudio {
		if repository, ok := service.repository.(interface {
			GuestUploadExists(context.Context, string) (bool, error)
		}); ok {
			exists, findErr := repository.GuestUploadExists(ctx, input.OwnerPrincipalID)
			if findErr != nil {
				return UploadResource{}, findErr
			}
			if exists {
				return UploadResource{}, ErrConflict
			}
		}
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
	assetRetentionUntil := retentionUntil
	if input.Purpose == PurposeInterviewTurnAudio {
		assetRetentionUntil = now.Add(24 * time.Hour)
	}
	resource := UploadResource{
		Asset: Asset{
			ID: assetID, OwnerPrincipalID: input.OwnerPrincipalID, Purpose: input.Purpose,
			State: "uploading", StorageDriver: service.store.Backend(), Bucket: service.config.Bucket,
			ObjectKey: objectKey, ContentType: input.ContentType, ExpectedSizeBytes: input.SizeBytes,
			ExpectedChecksumSHA256: input.ChecksumSHA256, RetentionUntil: &assetRetentionUntil,
			CreatedAt: now, UpdatedAt: now,
		},
		Upload: Upload{
			ID: uploadID, AssetID: assetID, ProviderUploadID: providerUpload.ID,
			State: "uploading", PartSizeBytes: service.config.PartSizeBytes,
			PartCount: partCount, ExpiresAt: retentionUntil,
			CreatedBySessionID: input.SessionID, InterviewSessionID: input.InterviewSessionID,
			IdempotencyKey: input.IdempotencyKey,
			CreatedAt:      now, UpdatedAt: now,
		},
	}
	if err := service.repository.InsertUpload(ctx, resource.Asset, resource.Upload); err != nil {
		_ = service.store.AbortMultipart(ctx, providerUpload)
		if errors.Is(err, ErrIdempotencyRace) {
			existing, findErr := service.repository.FindByIdempotency(ctx, input.SessionID, input.IdempotencyKey)
			if findErr == nil && sameUploadRequest(existing.Asset, input) && existing.Upload.InterviewSessionID == input.InterviewSessionID {
				return existing, nil
			}
			return UploadResource{}, ErrConflict
		}
		return UploadResource{}, err
	}
	return resource, nil
}

func (service *Service) GetUpload(ctx context.Context, ownerPrincipalID string, uploadID string) (UploadResource, []UploadedPart, error) {
	resource, err := service.repository.GetUpload(ctx, strings.TrimSpace(ownerPrincipalID), strings.TrimSpace(uploadID))
	if err != nil {
		return UploadResource{}, nil, err
	}
	parts, err := service.store.ListParts(ctx, multipartUpload(resource))
	if errors.Is(err, storage.ErrNotFound) && (resource.Upload.State == "completed" || resource.Upload.State == "aborted") {
		return resource, []UploadedPart{}, nil
	}
	if err != nil {
		return UploadResource{}, nil, mapStorageError(err)
	}
	return resource, uploadedPartsFromStorage(parts), nil
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
				Request: SignedRequest{
					Method:    http.MethodPut,
					Headers:   map[string][]string{"Content-Type": {resource.Asset.ContentType}},
					ExpiresAt: service.config.Now().Add(service.signedTTL(resource.Upload.ExpiresAt)).UTC(),
				},
				Local: true,
			})
			continue
		}
		if signErr != nil {
			return nil, mapStorageError(signErr)
		}
		result = append(result, SignedPart{Descriptor: descriptor, Request: signedRequestFromStorage(presigned)})
	}
	return result, nil
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
	completedResource, err := service.repository.MarkCompleted(ctx, uploadID, verifiedObjectFromStorage(info), service.config.Now().UTC())
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

func (service *Service) Download(ctx context.Context, input DownloadInput) (Download, error) {
	if strings.ToLower(strings.TrimSpace(input.OwnerKind)) != "user" {
		return Download{}, ErrAccountRequired
	}
	asset, err := service.repository.GetReadyAsset(ctx, strings.TrimSpace(input.OwnerPrincipalID), strings.TrimSpace(input.AssetID))
	if err != nil {
		return Download{}, err
	}
	presigned, signErr := service.store.PresignGet(ctx, asset.ObjectKey, service.config.SignedRequestTTL)
	if errors.Is(signErr, storage.ErrUnsupported) && service.store.Backend() == storage.BackendLocal {
		return Download{Asset: asset, Request: SignedRequest{
			Method: http.MethodGet, Headers: map[string][]string{},
			ExpiresAt: service.config.Now().Add(service.config.SignedRequestTTL).UTC(),
		}, Local: true}, nil
	}
	if signErr != nil {
		return Download{}, mapStorageError(signErr)
	}
	return Download{Asset: asset, Request: signedRequestFromStorage(presigned)}, nil
}

func multipartUpload(resource UploadResource) storage.MultipartUpload {
	return storage.MultipartUpload{
		ID: resource.Upload.ProviderUploadID, Key: resource.Asset.ObjectKey,
		ContentType: resource.Asset.ContentType, Size: resource.Asset.ExpectedSizeBytes,
		SHA256:   resource.Asset.ExpectedChecksumSHA256,
		Metadata: map[string]string{"asset-id": resource.Asset.ID, "owner-principal-id": resource.Asset.OwnerPrincipalID, "purpose": resource.Asset.Purpose},
	}
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
