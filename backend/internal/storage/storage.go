package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	BackendLocal = "local"
	BackendS3    = "s3"
)

var (
	ErrNotFound         = errors.New("storage object not found")
	ErrConflict         = errors.New("storage object conflicts with existing data")
	ErrInvalidKey       = errors.New("storage object key is invalid")
	ErrInvalidRequest   = errors.New("storage request is invalid")
	ErrChecksumMismatch = errors.New("storage checksum does not match")
	ErrSizeMismatch     = errors.New("storage size does not match")
	ErrUnsupported      = errors.New("storage operation is unsupported")
)

var (
	objectKeySegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	generatedSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)
	extensionPattern        = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	checksumPattern         = regexp.MustCompile(`^[a-f0-9]{64}$`)
	metadataNamePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// Store owns immutable media objects. Object keys are logical keys and never
// include a bucket name, a local root, or a configured S3 prefix.
//
// All mutating calls are safe to retry. A retry may return the already stored
// object when its size and SHA-256 are identical; it returns ErrConflict when
// the same key names different bytes.
type Store interface {
	Backend() string
	Put(context.Context, PutRequest, io.Reader) (ObjectInfo, error)
	Open(context.Context, string) (io.ReadCloser, ObjectInfo, error)
	Stat(context.Context, string) (ObjectInfo, error)
	Delete(context.Context, string) error

	CreateMultipart(context.Context, MultipartRequest) (MultipartUpload, error)
	PutPart(context.Context, MultipartUpload, PartRequest, io.Reader) (PartInfo, error)
	PresignUploadPart(context.Context, MultipartUpload, PartRequest, time.Duration) (PresignedRequest, error)
	ListParts(context.Context, MultipartUpload) ([]PartInfo, error)
	CompleteMultipart(context.Context, MultipartUpload, []CompletedPart) (ObjectInfo, error)
	AbortMultipart(context.Context, MultipartUpload) error

	PresignGet(context.Context, string, time.Duration) (PresignedRequest, error)
}

var _ Store = (*LocalStore)(nil)
var _ Store = (*S3Store)(nil)

type PutRequest struct {
	Key         string
	ContentType string
	Size        int64
	SHA256      string
	Metadata    map[string]string
}

type MultipartRequest = PutRequest

type ObjectInfo struct {
	Key          string
	ContentType  string
	Size         int64
	SHA256       string
	ETag         string
	LastModified time.Time
	Metadata     map[string]string
}

type MultipartUpload struct {
	ID          string
	Key         string
	ContentType string
	Size        int64
	SHA256      string
	Metadata    map[string]string
}

type PartRequest struct {
	Number int32
	Size   int64
	SHA256 string
}

type PartInfo struct {
	Number       int32
	Size         int64
	SHA256       string
	ETag         string
	LastModified time.Time
}

type CompletedPart struct {
	Number int32  `json:"partNumber"`
	ETag   string `json:"etag"`
	SHA256 string `json:"checksumSha256,omitempty"`
}

type PresignedRequest struct {
	Method    string
	URL       string
	Headers   http.Header
	ExpiresAt time.Time
}

// NewObjectKey creates an opaque server-generated key. Callers supply only
// validated ownership and purpose segments; no client-controlled path is ever
// accepted as a storage location.
func NewObjectKey(principalID string, purpose string, extension string) (string, error) {
	principalID = strings.TrimSpace(principalID)
	purpose = strings.ToLower(strings.TrimSpace(purpose))
	extension = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
	if !generatedSegmentPattern.MatchString(principalID) || !generatedSegmentPattern.MatchString(purpose) || !extensionPattern.MatchString(extension) {
		return "", ErrInvalidKey
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}
	key := path.Join("v1", principalID, purpose, hex.EncodeToString(random)+"."+extension)
	if err := ValidateObjectKey(key); err != nil {
		return "", err
	}
	return key, nil
}

// ValidateObjectKey rejects absolute paths, dot segments, backslashes,
// control characters, and ambiguous normalized paths.
func ValidateObjectKey(key string) error {
	if key == "" || len(key) > 1024 || strings.TrimSpace(key) != key || strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, "\\") {
		return ErrInvalidKey
	}
	if path.Clean(key) != key {
		return ErrInvalidKey
	}
	segments := strings.Split(key, "/")
	if len(segments) == 0 {
		return ErrInvalidKey
	}
	for _, segment := range segments {
		if segment == "." || segment == ".." || !objectKeySegmentPattern.MatchString(segment) {
			return ErrInvalidKey
		}
	}
	return nil
}

func validatePutRequest(request PutRequest) (PutRequest, error) {
	if err := ValidateObjectKey(request.Key); err != nil {
		return PutRequest{}, err
	}
	request.ContentType = strings.ToLower(strings.TrimSpace(strings.Split(request.ContentType, ";")[0]))
	request.SHA256 = strings.ToLower(strings.TrimSpace(request.SHA256))
	if request.Size < 0 || request.ContentType == "" || len(request.ContentType) > 255 || !checksumPattern.MatchString(request.SHA256) {
		return PutRequest{}, ErrInvalidRequest
	}
	metadata, err := normalizeMetadata(request.Metadata)
	if err != nil {
		return PutRequest{}, err
	}
	request.Metadata = metadata
	return request, nil
}

func validateUpload(upload MultipartUpload) (MultipartUpload, error) {
	request, err := validatePutRequest(PutRequest{
		Key: upload.Key, ContentType: upload.ContentType, Size: upload.Size,
		SHA256: upload.SHA256, Metadata: upload.Metadata,
	})
	if err != nil || !validOpaqueID(upload.ID) {
		return MultipartUpload{}, ErrInvalidRequest
	}
	upload.Key = request.Key
	upload.ContentType = request.ContentType
	upload.Size = request.Size
	upload.SHA256 = request.SHA256
	upload.Metadata = request.Metadata
	return upload, nil
}

func validatePartRequest(request PartRequest) (PartRequest, error) {
	request.SHA256 = strings.ToLower(strings.TrimSpace(request.SHA256))
	if request.Number < 1 || request.Number > 10000 || request.Size < 0 || !checksumPattern.MatchString(request.SHA256) {
		return PartRequest{}, ErrInvalidRequest
	}
	return request, nil
}

func validateCompletedParts(parts []CompletedPart) ([]CompletedPart, error) {
	if len(parts) == 0 || len(parts) > 10000 {
		return nil, ErrInvalidRequest
	}
	copyParts := append([]CompletedPart(nil), parts...)
	sort.Slice(copyParts, func(i, j int) bool { return copyParts[i].Number < copyParts[j].Number })
	for index := range copyParts {
		part := &copyParts[index]
		part.ETag = strings.TrimSpace(part.ETag)
		part.SHA256 = strings.ToLower(strings.TrimSpace(part.SHA256))
		if part.Number < 1 || part.Number > 10000 || part.ETag == "" || (part.SHA256 != "" && !checksumPattern.MatchString(part.SHA256)) {
			return nil, ErrInvalidRequest
		}
		if index > 0 && copyParts[index-1].Number == part.Number {
			return nil, ErrInvalidRequest
		}
	}
	return copyParts, nil
}

func normalizeMetadata(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(metadata))
	for rawName, rawValue := range metadata {
		name := strings.ToLower(strings.TrimSpace(rawName))
		value := strings.TrimSpace(rawValue)
		if !metadataNamePattern.MatchString(name) || len(value) > 1024 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, ErrInvalidRequest
		}
		out[name] = value
	}
	return out, nil
}

func validOpaqueID(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	return true
}

func normalizeTTL(value time.Duration, fallback time.Duration) (time.Duration, error) {
	if value == 0 {
		value = fallback
	}
	if value < time.Minute || value > 24*time.Hour {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func cloneMetadata(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
