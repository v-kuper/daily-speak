package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	s3MetadataSHA256 = "daily-speaking-sha256"
	s3MetadataSize   = "daily-speaking-size"
)

type S3Store struct {
	client     *s3.Client
	presigner  *s3.PresignClient
	bucket     string
	prefix     string
	presignTTL time.Duration
	now        func() time.Time
}

func New(ctx context.Context, config Config) (Store, error) {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Backend == BackendLocal {
		return NewLocal(config.LocalDir)
	}
	return NewS3(ctx, config)
}

func NewS3(ctx context.Context, config Config) (*S3Store, error) {
	config = config.withDefaults()
	if config.Backend != BackendS3 {
		return nil, errors.New("s3 storage requires MEDIA_STORAGE_DRIVER=s3")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(config.S3Region)}
	if config.S3AccessKeyID != "" {
		provider := credentials.NewStaticCredentialsProvider(config.S3AccessKeyID, config.S3SecretAccessKey, config.S3SessionToken)
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(provider))
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		options.UsePathStyle = config.S3ForcePathStyle
		if config.S3Endpoint != "" {
			options.BaseEndpoint = aws.String(strings.TrimSuffix(config.S3Endpoint, "/"))
		}
	})
	return &S3Store{
		client: client, presigner: s3.NewPresignClient(client), bucket: config.S3Bucket,
		prefix: config.KeyPrefix, presignTTL: config.PresignTTL, now: time.Now,
	}, nil
}

func (store *S3Store) Backend() string { return BackendS3 }

func (store *S3Store) Put(ctx context.Context, request PutRequest, body io.Reader) (ObjectInfo, error) {
	request, err := validatePutRequest(request)
	if err != nil || body == nil {
		return ObjectInfo{}, ErrInvalidRequest
	}
	if existing, statErr := store.Stat(ctx, request.Key); statErr == nil {
		if existing.Size == request.Size && existing.SHA256 == request.SHA256 {
			return existing, nil
		}
		return ObjectInfo{}, ErrConflict
	} else if !errors.Is(statErr, ErrNotFound) {
		return ObjectInfo{}, statErr
	}
	file, cleanup, err := verifiedTemporaryFile(ctx, body, request.Size, request.SHA256)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer cleanup()
	metadata := s3Metadata(request.Metadata, request.Size, request.SHA256)
	checksum, err := checksumHexToBase64(request.SHA256)
	if err != nil {
		return ObjectInfo{}, err
	}
	output, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(request.Key)), Body: file,
		ContentLength: aws.Int64(request.Size), ContentType: aws.String(request.ContentType),
		ChecksumSHA256: aws.String(checksum), Metadata: metadata,
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("put S3 object: %w", err)
	}
	info, err := store.Stat(ctx, request.Key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if info.Size != request.Size || info.SHA256 != request.SHA256 {
		_ = store.Delete(ctx, request.Key)
		if info.Size != request.Size {
			return ObjectInfo{}, ErrSizeMismatch
		}
		return ObjectInfo{}, ErrChecksumMismatch
	}
	if output.ETag != nil {
		info.ETag = aws.ToString(output.ETag)
	}
	return info, nil
}

func (store *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ValidateObjectKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	output, err := store.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(key))})
	if isS3NotFound(err) {
		return nil, ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("open S3 object: %w", err)
	}
	info, err := objectInfoFromS3(key, output.ContentType, output.ContentLength, output.ChecksumSHA256, output.ETag, output.LastModified, output.Metadata)
	if err != nil {
		_ = output.Body.Close()
		return nil, ObjectInfo{}, err
	}
	return output.Body, info, nil
}

func (store *S3Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ValidateObjectKey(key); err != nil {
		return ObjectInfo{}, err
	}
	output, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(key))})
	if isS3NotFound(err) {
		return ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat S3 object: %w", err)
	}
	return objectInfoFromS3(key, output.ContentType, output.ContentLength, output.ChecksumSHA256, output.ETag, output.LastModified, output.Metadata)
}

func (store *S3Store) Delete(ctx context.Context, key string) error {
	if err := ValidateObjectKey(key); err != nil {
		return err
	}
	_, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(key))})
	if err != nil && !isS3NotFound(err) {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (store *S3Store) CreateMultipart(ctx context.Context, request MultipartRequest) (MultipartUpload, error) {
	request, err := validatePutRequest(request)
	if err != nil {
		return MultipartUpload{}, err
	}
	output, err := store.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(request.Key)),
		ContentType: aws.String(request.ContentType), Metadata: s3Metadata(request.Metadata, request.Size, request.SHA256),
	})
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("create S3 multipart upload: %w", err)
	}
	uploadID := aws.ToString(output.UploadId)
	if !validOpaqueID(uploadID) {
		return MultipartUpload{}, errors.New("S3 returned an invalid multipart upload ID")
	}
	return MultipartUpload{
		ID: uploadID, Key: request.Key, ContentType: request.ContentType, Size: request.Size,
		SHA256: request.SHA256, Metadata: cloneMetadata(request.Metadata),
	}, nil
}

func (store *S3Store) PutPart(ctx context.Context, upload MultipartUpload, request PartRequest, body io.Reader) (PartInfo, error) {
	upload, err := validateUpload(upload)
	if err != nil {
		return PartInfo{}, err
	}
	request, err = validatePartRequest(request)
	if err != nil || body == nil {
		return PartInfo{}, ErrInvalidRequest
	}
	file, cleanup, err := verifiedTemporaryFile(ctx, body, request.Size, request.SHA256)
	if err != nil {
		return PartInfo{}, err
	}
	defer cleanup()
	checksum, err := checksumHexToBase64(request.SHA256)
	if err != nil {
		return PartInfo{}, err
	}
	output, err := store.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(upload.Key)), UploadId: aws.String(upload.ID),
		PartNumber: aws.Int32(request.Number), Body: file, ContentLength: aws.Int64(request.Size), ChecksumSHA256: aws.String(checksum),
	})
	if err != nil {
		return PartInfo{}, fmt.Errorf("put S3 multipart part: %w", err)
	}
	return PartInfo{
		Number: request.Number, Size: request.Size, SHA256: request.SHA256,
		ETag: aws.ToString(output.ETag), LastModified: store.now().UTC(),
	}, nil
}

func (store *S3Store) PresignUploadPart(ctx context.Context, upload MultipartUpload, request PartRequest, ttl time.Duration) (PresignedRequest, error) {
	upload, err := validateUpload(upload)
	if err != nil {
		return PresignedRequest{}, err
	}
	request, err = validatePartRequest(request)
	if err != nil {
		return PresignedRequest{}, ErrInvalidRequest
	}
	ttl, err = normalizeTTL(ttl, store.presignTTL)
	if err != nil {
		return PresignedRequest{}, err
	}
	checksum, err := checksumHexToBase64(request.SHA256)
	if err != nil {
		return PresignedRequest{}, err
	}
	output, err := store.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(upload.Key)), UploadId: aws.String(upload.ID),
		PartNumber: aws.Int32(request.Number), ContentLength: aws.Int64(request.Size), ChecksumSHA256: aws.String(checksum),
	}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign S3 upload part: %w", err)
	}
	return PresignedRequest{
		Method: output.Method, URL: output.URL, Headers: output.SignedHeader.Clone(), ExpiresAt: store.now().UTC().Add(ttl),
	}, nil
}

func (store *S3Store) ListParts(ctx context.Context, upload MultipartUpload) ([]PartInfo, error) {
	upload, err := validateUpload(upload)
	if err != nil {
		return nil, err
	}
	paginator := s3.NewListPartsPaginator(store.client, &s3.ListPartsInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(upload.Key)), UploadId: aws.String(upload.ID),
	})
	parts := []PartInfo{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if isS3NoSuchUpload(err) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("list S3 multipart parts: %w", err)
		}
		for _, part := range page.Parts {
			checksum, err := checksumBase64ToHex(aws.ToString(part.ChecksumSHA256))
			if err != nil && aws.ToString(part.ChecksumSHA256) != "" {
				return nil, err
			}
			parts = append(parts, PartInfo{
				Number: aws.ToInt32(part.PartNumber), Size: aws.ToInt64(part.Size), SHA256: checksum,
				ETag: aws.ToString(part.ETag), LastModified: aws.ToTime(part.LastModified).UTC(),
			})
		}
	}
	return parts, nil
}

func (store *S3Store) CompleteMultipart(ctx context.Context, upload MultipartUpload, completed []CompletedPart) (ObjectInfo, error) {
	upload, err := validateUpload(upload)
	if err != nil {
		return ObjectInfo{}, err
	}
	completed, err = validateCompletedParts(completed)
	if err != nil {
		return ObjectInfo{}, err
	}
	providerParts := make([]types.CompletedPart, 0, len(completed))
	for _, part := range completed {
		providerPart := types.CompletedPart{PartNumber: aws.Int32(part.Number), ETag: aws.String(part.ETag)}
		if part.SHA256 != "" {
			checksum, err := checksumHexToBase64(part.SHA256)
			if err != nil {
				return ObjectInfo{}, err
			}
			providerPart.ChecksumSHA256 = aws.String(checksum)
		}
		providerParts = append(providerParts, providerPart)
	}
	_, err = store.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(upload.Key)), UploadId: aws.String(upload.ID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: providerParts},
	})
	if err != nil {
		// A retry after S3 completed the object but before PostgreSQL recorded it
		// commonly receives NoSuchUpload. Treat an already verified object as the
		// successful result.
		if existing, verifyErr := store.verifyStoredObject(ctx, upload.Key, upload.Size, upload.SHA256); verifyErr == nil {
			return existing, nil
		} else if !errors.Is(verifyErr, ErrNotFound) {
			return ObjectInfo{}, verifyErr
		}
		return ObjectInfo{}, fmt.Errorf("complete S3 multipart upload: %w", err)
	}
	info, err := store.verifyStoredObject(ctx, upload.Key, upload.Size, upload.SHA256)
	if err != nil {
		if errors.Is(err, ErrSizeMismatch) || errors.Is(err, ErrChecksumMismatch) {
			_ = store.Delete(ctx, upload.Key)
		}
		return ObjectInfo{}, err
	}
	return info, nil
}

func (store *S3Store) AbortMultipart(ctx context.Context, upload MultipartUpload) error {
	upload, err := validateUpload(upload)
	if err != nil {
		return err
	}
	_, err = store.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(upload.Key)), UploadId: aws.String(upload.ID),
	})
	if err != nil && !isS3NoSuchUpload(err) && !isS3NotFound(err) {
		return fmt.Errorf("abort S3 multipart upload: %w", err)
	}
	return nil
}

func (store *S3Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (PresignedRequest, error) {
	if err := ValidateObjectKey(key); err != nil {
		return PresignedRequest{}, err
	}
	if _, err := store.Stat(ctx, key); err != nil {
		return PresignedRequest{}, err
	}
	ttl, err := normalizeTTL(ttl, store.presignTTL)
	if err != nil {
		return PresignedRequest{}, err
	}
	output, err := store.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(store.remoteKey(key)),
	}, func(options *s3.PresignOptions) { options.Expires = ttl })
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign S3 get: %w", err)
	}
	return PresignedRequest{Method: output.Method, URL: output.URL, Headers: output.SignedHeader.Clone(), ExpiresAt: store.now().UTC().Add(ttl)}, nil
}

func (store *S3Store) verifyStoredObject(ctx context.Context, key string, expectedSize int64, expectedSHA string) (ObjectInfo, error) {
	reader, info, err := store.Open(ctx, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	hash := sha256.New()
	size, copyErr := copyWithContext(ctx, hash, io.LimitReader(reader, expectedSize+1))
	closeErr := reader.Close()
	if copyErr != nil {
		return ObjectInfo{}, copyErr
	}
	if closeErr != nil {
		return ObjectInfo{}, closeErr
	}
	if size != expectedSize {
		return ObjectInfo{}, ErrSizeMismatch
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		return ObjectInfo{}, ErrChecksumMismatch
	}
	info.Size = size
	info.SHA256 = expectedSHA
	return info, nil
}

func (store *S3Store) remoteKey(logicalKey string) string {
	if store.prefix == "" {
		return logicalKey
	}
	return path.Join(store.prefix, logicalKey)
}

func objectInfoFromS3(key string, contentType *string, contentLength *int64, checksumBase64 *string, etag *string, lastModified *time.Time, metadata map[string]string) (ObjectInfo, error) {
	shaHex := strings.ToLower(strings.TrimSpace(metadata[s3MetadataSHA256]))
	if shaHex == "" && checksumBase64 != nil {
		var err error
		shaHex, err = checksumBase64ToHex(aws.ToString(checksumBase64))
		if err != nil {
			return ObjectInfo{}, err
		}
	}
	if !checksumPattern.MatchString(shaHex) {
		return ObjectInfo{}, errors.New("S3 object is missing trusted SHA-256 metadata")
	}
	size := aws.ToInt64(contentLength)
	if storedSize := strings.TrimSpace(metadata[s3MetadataSize]); storedSize != "" {
		parsed, err := strconv.ParseInt(storedSize, 10, 64)
		if err != nil || parsed != size {
			return ObjectInfo{}, ErrSizeMismatch
		}
	}
	userMetadata := cloneMetadata(metadata)
	delete(userMetadata, s3MetadataSHA256)
	delete(userMetadata, s3MetadataSize)
	return ObjectInfo{
		Key: key, ContentType: aws.ToString(contentType), Size: size, SHA256: shaHex,
		ETag: aws.ToString(etag), LastModified: aws.ToTime(lastModified).UTC(), Metadata: userMetadata,
	}, nil
}

func s3Metadata(metadata map[string]string, size int64, sha string) map[string]string {
	out := cloneMetadata(metadata)
	out[s3MetadataSHA256] = sha
	out[s3MetadataSize] = strconv.FormatInt(size, 10)
	return out
}

func verifiedTemporaryFile(ctx context.Context, body io.Reader, expectedSize int64, expectedSHA string) (*os.File, func(), error) {
	file, err := os.CreateTemp("", "daily-speaking-media-*.tmp")
	if err != nil {
		return nil, nil, fmt.Errorf("create verified upload temp file: %w", err)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	if _, _, err := copyVerified(ctx, file, body, expectedSize, expectedSHA); err != nil {
		cleanup()
		return nil, nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("rewind verified upload: %w", err)
	}
	return file, cleanup, nil
}

func checksumHexToBase64(value string) (string, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", ErrInvalidRequest
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}

func checksumBase64ToHex(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("S3 returned an invalid SHA-256 checksum")
	}
	return hex.EncodeToString(decoded), nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket", "404":
			return true
		}
	}
	return false
}

func isS3NoSuchUpload(err error) bool {
	if err == nil {
		return false
	}
	var apiError smithy.APIError
	return errors.As(err, &apiError) && apiError.ErrorCode() == "NoSuchUpload"
}
