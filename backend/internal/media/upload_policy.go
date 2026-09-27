package media

import (
	"regexp"
	"strings"
	"time"
)

var (
	checksumPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

func normalizeCreateInput(input CreateUploadInput) CreateUploadInput {
	input.OwnerPrincipalID = strings.TrimSpace(input.OwnerPrincipalID)
	input.OwnerKind = strings.ToLower(strings.TrimSpace(input.OwnerKind))
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Purpose = strings.ToLower(strings.TrimSpace(input.Purpose))
	input.ContentType = strings.ToLower(strings.TrimSpace(strings.Split(input.ContentType, ";")[0]))
	input.ChecksumSHA256 = strings.ToLower(strings.TrimSpace(input.ChecksumSHA256))
	return input
}

func applyCreateOwnerPolicy(input CreateUploadInput) (CreateUploadInput, error) {
	switch input.OwnerKind {
	case "guest":
		if input.Purpose != PurposeRecordingAudio {
			return CreateUploadInput{}, ErrGuestRestricted
		}
		input.Purpose = PurposeGuestPreviewAudio
	case "user":
		if input.Purpose == PurposeGuestPreviewAudio {
			return CreateUploadInput{}, ErrInvalidRequest
		}
	default:
		return CreateUploadInput{}, ErrInvalidRequest
	}
	return input, nil
}

func validateCreateInput(input CreateUploadInput) (string, error) {
	if input.OwnerPrincipalID == "" || input.SessionID == "" || !idempotencyPattern.MatchString(input.IdempotencyKey) || input.SizeBytes <= 0 {
		return "", ErrInvalidRequest
	}
	if !checksumPattern.MatchString(input.ChecksumSHA256) {
		return "", ErrChecksumMismatch
	}
	switch input.Purpose {
	case PurposeRecordingAudio, PurposeGuestPreviewAudio:
		allowed := map[string]bool{
			"audio/webm": true, "video/webm": true, "audio/mp4": true,
			"audio/x-m4a": true, "video/mp4": true, "audio/ogg": true,
			"video/ogg": true, "audio/wav": true, "audio/x-wav": true,
			"audio/vnd.wave": true, "audio/mpeg": true,
		}
		if !allowed[input.ContentType] {
			return "", ErrUnsupportedType
		}
		extension := ResolveAudioExtension(input.ContentType)
		maxBytes := int64(MaxAudioUploadBytes)
		if input.Purpose == PurposeGuestPreviewAudio {
			maxBytes = 10 * 1024 * 1024
		}
		if input.SizeBytes > maxBytes {
			return "", ErrPayloadTooLarge
		}
		return extension, nil
	case PurposeRecordingPhoto:
		extensions := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp", "image/gif": "gif"}
		extension := extensions[input.ContentType]
		if extension == "" {
			return "", ErrUnsupportedType
		}
		if input.SizeBytes > MaxPhotoUploadBytes {
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
