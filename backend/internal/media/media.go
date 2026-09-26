package media

import (
	"context"
	"errors"
	"io"
	"time"

	"daily-speaking-practice/backend/internal/storage"
)

var (
	ErrNotFound         = errors.New("media resource not found")
	ErrInvalidRequest   = errors.New("media request is invalid")
	ErrConflict         = errors.New("media resource conflict")
	ErrExpired          = errors.New("media upload expired")
	ErrStorage          = errors.New("media storage unavailable")
	ErrIdempotencyRace  = errors.New("media idempotency conflict")
	ErrUnsupportedType  = errors.New("media type is unsupported")
	ErrPayloadTooLarge  = errors.New("media payload is too large")
	ErrChecksumMismatch = errors.New("media checksum does not match")
	ErrSizeMismatch     = errors.New("media size does not match")
)

const (
	PurposeRecordingAudio = "recording_audio"
	PurposeRecordingPhoto = "recording_photo"
)

type Asset struct {
	ID                     string
	OwnerPrincipalID       string
	Purpose                string
	State                  string
	StorageDriver          string
	Bucket                 string
	ObjectKey              string
	ContentType            string
	ExpectedSizeBytes      int64
	VerifiedSizeBytes      *int64
	ExpectedChecksumSHA256 string
	VerifiedChecksumSHA256 *string
	ETag                   *string
	RetentionUntil         *time.Time
	VerifiedAt             *time.Time
	AttachedAt             *time.Time
	DeletedAt              *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type Upload struct {
	ID                 string
	AssetID            string
	ProviderUploadID   string
	State              string
	PartSizeBytes      int64
	PartCount          int
	ExpiresAt          time.Time
	CreatedBySessionID string
	IdempotencyKey     string
	CompletedAt        *time.Time
	AbortedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Part struct {
	UploadID       string
	PartNumber     int
	SizeBytes      int64
	ETag           string
	ChecksumSHA256 string
	VerifiedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type UploadResource struct {
	Asset  Asset
	Upload Upload
}

type CreateUploadInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	Purpose          string
	ContentType      string
	SizeBytes        int64
	ChecksumSHA256   string
}

type PartDescriptor struct {
	PartNumber     int    `json:"partNumber"`
	SizeBytes      int64  `json:"sizeBytes"`
	ChecksumSHA256 string `json:"checksumSha256"`
}

type SignedPart struct {
	Descriptor PartDescriptor
	Request    storage.PresignedRequest
	Local      bool
}

type CompletedPart struct {
	PartNumber     int    `json:"partNumber"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksumSha256,omitempty"`
}

type Download struct {
	Asset   Asset
	Request storage.PresignedRequest
	Local   bool
}

type Content struct {
	Body io.ReadCloser
	Info storage.ObjectInfo
}

type Repository interface {
	FindByIdempotency(context.Context, string, string) (UploadResource, error)
	InsertUpload(context.Context, Asset, Upload) error
	GetUpload(context.Context, string, string) (UploadResource, error)
	GetUploadByID(context.Context, string) (UploadResource, error)
	UpsertPart(context.Context, Part) error
	ClaimCompleting(context.Context, string) error
	ClaimAborting(context.Context, string, *time.Time) error
	MarkCompleted(context.Context, string, storage.ObjectInfo, time.Time) (UploadResource, error)
	MarkAborted(context.Context, string, time.Time) (UploadResource, error)
	GetReadyAsset(context.Context, string, string) (Asset, error)
	GetReadyAssetByID(context.Context, string) (Asset, error)
}

type Config struct {
	Bucket             string
	UploadTTL          time.Duration
	SignedRequestTTL   time.Duration
	PartSizeBytes      int64
	MaxPartDescriptors int
	Now                func() time.Time
}

type Service struct {
	repository Repository
	store      storage.Store
	config     Config
}
