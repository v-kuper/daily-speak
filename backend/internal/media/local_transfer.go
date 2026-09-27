package media

import (
	"context"
	"io"
	"strings"

	"daily-speaking-practice/backend/internal/storage"
)

func (service *Service) PutLocalPart(ctx context.Context, uploadID string, descriptor PartDescriptor, bodySize int64, body io.Reader) (UploadedPart, error) {
	if service.store == nil || service.store.Backend() != storage.BackendLocal || body == nil || bodySize != descriptor.SizeBytes {
		return UploadedPart{}, ErrInvalidRequest
	}
	resource, err := service.repository.GetUploadByID(ctx, strings.TrimSpace(uploadID))
	if err != nil {
		return UploadedPart{}, err
	}
	if err := service.validateActiveUpload(resource); err != nil || !service.validPartDescriptor(resource, descriptor) {
		if err != nil {
			return UploadedPart{}, err
		}
		return UploadedPart{}, ErrInvalidRequest
	}
	part, err := service.store.PutPart(ctx, multipartUpload(resource), storage.PartRequest{
		Number: int32(descriptor.PartNumber), Size: descriptor.SizeBytes,
		SHA256: strings.ToLower(strings.TrimSpace(descriptor.ChecksumSHA256)),
	}, body)
	if err != nil {
		return UploadedPart{}, mapStorageError(err)
	}
	now := service.config.Now().UTC()
	if err := service.repository.UpsertPart(ctx, Part{
		UploadID: uploadID, PartNumber: descriptor.PartNumber, SizeBytes: part.Size,
		ETag: part.ETag, ChecksumSHA256: part.SHA256, VerifiedAt: &now,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return UploadedPart{}, err
	}
	return uploadedPartFromStorage(part), nil
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
	return Content{Body: body, Info: contentInfoFromStorage(info)}, nil
}
