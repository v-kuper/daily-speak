package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	localMetadataDirectory  = ".daily-speaking-metadata"
	localMultipartDirectory = ".daily-speaking-multipart"
)

type LocalStore struct {
	root string
	now  func() time.Time
}

type localObjectMetadata struct {
	Key          string            `json:"key"`
	ContentType  string            `json:"contentType"`
	Size         int64             `json:"size"`
	SHA256       string            `json:"sha256"`
	ETag         string            `json:"etag"`
	LastModified time.Time         `json:"lastModified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type localUploadMetadata struct {
	Upload MultipartUpload `json:"upload"`
}

type localPartMetadata struct {
	Part PartInfo `json:"part"`
}

func NewLocal(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("local storage root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root symlinks: %w", err)
	}
	return &LocalStore{root: filepath.Clean(resolved), now: time.Now}, nil
}

func (store *LocalStore) Backend() string { return BackendLocal }

func (store *LocalStore) Put(ctx context.Context, request PutRequest, body io.Reader) (ObjectInfo, error) {
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
	target, err := store.objectPath(request.Key, true)
	if err != nil {
		return ObjectInfo{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".put-*.tmp")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("create local object temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	actualSize, actualSHA, writeErr := copyVerified(ctx, temporary, body, request.Size, request.SHA256)
	if syncErr := temporary.Sync(); writeErr == nil && syncErr != nil {
		writeErr = syncErr
	}
	if closeErr := temporary.Close(); writeErr == nil && closeErr != nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return ObjectInfo{}, writeErr
	}
	if err := os.Chmod(temporaryPath, 0o640); err != nil {
		return ObjectInfo{}, fmt.Errorf("set local object permissions: %w", err)
	}
	if err := publishExclusive(temporaryPath, target); err != nil {
		if existing, statErr := store.Stat(ctx, request.Key); statErr == nil && existing.Size == actualSize && existing.SHA256 == actualSHA {
			return existing, nil
		}
		if errors.Is(err, os.ErrExist) {
			return ObjectInfo{}, ErrConflict
		}
		return ObjectInfo{}, fmt.Errorf("publish local object: %w", err)
	}
	info := ObjectInfo{
		Key: request.Key, ContentType: request.ContentType, Size: actualSize,
		SHA256: actualSHA, ETag: quoteETag(actualSHA), LastModified: store.now().UTC(),
		Metadata: cloneMetadata(request.Metadata),
	}
	if err := store.writeObjectMetadata(info); err != nil {
		_ = os.Remove(target)
		return ObjectInfo{}, err
	}
	return info, nil
}

func (store *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := store.Stat(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	objectPath, err := store.objectPath(key, false)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(objectPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return nil, ObjectInfo{}, fmt.Errorf("open local object: %w", err)
	}
	return file, info, nil
}

func (store *LocalStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	objectPath, err := store.objectPath(key, false)
	if err != nil {
		return ObjectInfo{}, err
	}
	fileInfo, err := os.Stat(objectPath)
	if errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat local object: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return ObjectInfo{}, ErrNotFound
	}
	var metadata localObjectMetadata
	if err := readJSONFile(store.objectMetadataPath(key), &metadata); err == nil && metadata.Key == key && metadata.Size == fileInfo.Size() && checksumPattern.MatchString(metadata.SHA256) {
		return objectInfoFromLocalMetadata(metadata), nil
	}
	// Legacy local objects have no sidecar. Hash them on first access and write
	// the sidecar so they participate in the same integrity contract.
	file, err := os.Open(objectPath)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("open legacy local object: %w", err)
	}
	hash := sha256.New()
	first := make([]byte, 512)
	read, readErr := io.ReadFull(file, first)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		_ = file.Close()
		return ObjectInfo{}, fmt.Errorf("read legacy local object: %w", readErr)
	}
	_, _ = hash.Write(first[:read])
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		return ObjectInfo{}, fmt.Errorf("hash legacy local object: %w", err)
	}
	if err := file.Close(); err != nil {
		return ObjectInfo{}, fmt.Errorf("close legacy local object: %w", err)
	}
	contentType := "application/octet-stream"
	if read > 0 {
		contentType = http.DetectContentType(first[:read])
	}
	info := ObjectInfo{
		Key: key, ContentType: contentType, Size: fileInfo.Size(), SHA256: hex.EncodeToString(hash.Sum(nil)),
		ETag: quoteETag(hex.EncodeToString(hash.Sum(nil))), LastModified: fileInfo.ModTime().UTC(), Metadata: map[string]string{},
	}
	_ = store.writeObjectMetadata(info)
	return info, nil
}

func (store *LocalStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	objectPath, err := store.objectPath(key, false)
	if err != nil {
		return err
	}
	if err := os.Remove(objectPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete local object: %w", err)
	}
	if err := os.Remove(store.objectMetadataPath(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete local object metadata: %w", err)
	}
	return nil
}

func (store *LocalStore) CreateMultipart(ctx context.Context, request MultipartRequest) (MultipartUpload, error) {
	request, err := validatePutRequest(request)
	if err != nil {
		return MultipartUpload{}, err
	}
	if err := ctx.Err(); err != nil {
		return MultipartUpload{}, err
	}
	upload := MultipartUpload{
		ID: uuid.NewString(), Key: request.Key, ContentType: request.ContentType,
		Size: request.Size, SHA256: request.SHA256, Metadata: cloneMetadata(request.Metadata),
	}
	directory := store.multipartPath(upload.ID)
	if err := os.MkdirAll(filepath.Join(directory, "parts"), 0o750); err != nil {
		return MultipartUpload{}, fmt.Errorf("create local multipart upload: %w", err)
	}
	if err := writeJSONAtomic(filepath.Join(directory, "upload.json"), localUploadMetadata{Upload: upload}); err != nil {
		_ = os.RemoveAll(directory)
		return MultipartUpload{}, err
	}
	return upload, nil
}

func (store *LocalStore) PutPart(ctx context.Context, upload MultipartUpload, request PartRequest, body io.Reader) (PartInfo, error) {
	stored, err := store.loadUpload(upload)
	if err != nil {
		return PartInfo{}, err
	}
	request, err = validatePartRequest(request)
	if err != nil || body == nil {
		return PartInfo{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return PartInfo{}, err
	}
	partPath := store.localPartPath(stored.ID, request.Number)
	metadataPath := partPath + ".json"
	var existing localPartMetadata
	if err := readJSONFile(metadataPath, &existing); err == nil {
		if existing.Part.Size == request.Size && existing.Part.SHA256 == request.SHA256 {
			return existing.Part, nil
		}
		return PartInfo{}, ErrConflict
	}
	temporary, err := os.CreateTemp(filepath.Dir(partPath), ".part-*.tmp")
	if err != nil {
		return PartInfo{}, fmt.Errorf("create local multipart part: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	actualSize, actualSHA, copyErr := copyVerified(ctx, temporary, body, request.Size, request.SHA256)
	if syncErr := temporary.Sync(); copyErr == nil && syncErr != nil {
		copyErr = syncErr
	}
	if closeErr := temporary.Close(); copyErr == nil && closeErr != nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return PartInfo{}, copyErr
	}
	if err := os.Chmod(temporaryPath, 0o640); err != nil {
		return PartInfo{}, err
	}
	if err := publishExclusive(temporaryPath, partPath); err != nil {
		var raced localPartMetadata
		if readJSONFile(metadataPath, &raced) == nil && raced.Part.Size == actualSize && raced.Part.SHA256 == actualSHA {
			return raced.Part, nil
		}
		return PartInfo{}, ErrConflict
	}
	part := PartInfo{Number: request.Number, Size: actualSize, SHA256: actualSHA, ETag: quoteETag(actualSHA), LastModified: store.now().UTC()}
	if err := writeJSONAtomic(metadataPath, localPartMetadata{Part: part}); err != nil {
		_ = os.Remove(partPath)
		return PartInfo{}, err
	}
	return part, nil
}

func (store *LocalStore) PresignUploadPart(context.Context, MultipartUpload, PartRequest, time.Duration) (PresignedRequest, error) {
	return PresignedRequest{}, ErrUnsupported
}

func (store *LocalStore) ListParts(ctx context.Context, upload MultipartUpload) ([]PartInfo, error) {
	stored, err := store.loadUpload(upload)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(store.multipartPath(stored.ID), "parts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("list local multipart parts: %w", err)
	}
	parts := make([]PartInfo, 0, len(entries)/2)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var metadata localPartMetadata
		if err := readJSONFile(filepath.Join(store.multipartPath(stored.ID), "parts", entry.Name()), &metadata); err != nil {
			return nil, fmt.Errorf("read local multipart part metadata: %w", err)
		}
		parts = append(parts, metadata.Part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	return parts, nil
}

func (store *LocalStore) CompleteMultipart(ctx context.Context, upload MultipartUpload, completed []CompletedPart) (ObjectInfo, error) {
	stored, err := store.loadUpload(upload)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if existing, statErr := store.Stat(ctx, upload.Key); statErr == nil && existing.Size == upload.Size && existing.SHA256 == upload.SHA256 {
				return existing, nil
			}
		}
		return ObjectInfo{}, err
	}
	completed, err = validateCompletedParts(completed)
	if err != nil {
		return ObjectInfo{}, err
	}
	parts, err := store.ListParts(ctx, stored)
	if err != nil {
		return ObjectInfo{}, err
	}
	if len(parts) != len(completed) {
		return ObjectInfo{}, ErrInvalidRequest
	}
	target, err := store.objectPath(stored.Key, true)
	if err != nil {
		return ObjectInfo{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".complete-*.tmp")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("create completed local object: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	hash := sha256.New()
	written := int64(0)
	for index, part := range parts {
		if part.Number != completed[index].Number || part.ETag != completed[index].ETag || (completed[index].SHA256 != "" && completed[index].SHA256 != part.SHA256) {
			_ = temporary.Close()
			return ObjectInfo{}, ErrInvalidRequest
		}
		partFile, err := os.Open(store.localPartPath(stored.ID, part.Number))
		if err != nil {
			_ = temporary.Close()
			return ObjectInfo{}, fmt.Errorf("open local multipart part: %w", err)
		}
		copied, copyErr := copyWithContext(ctx, io.MultiWriter(temporary, hash), partFile)
		closeErr := partFile.Close()
		written += copied
		if copyErr != nil {
			_ = temporary.Close()
			return ObjectInfo{}, copyErr
		}
		if closeErr != nil {
			_ = temporary.Close()
			return ObjectInfo{}, closeErr
		}
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ObjectInfo{}, err
	}
	if err := temporary.Close(); err != nil {
		return ObjectInfo{}, err
	}
	actualSHA := hex.EncodeToString(hash.Sum(nil))
	if written != stored.Size {
		return ObjectInfo{}, ErrSizeMismatch
	}
	if actualSHA != stored.SHA256 {
		return ObjectInfo{}, ErrChecksumMismatch
	}
	if err := os.Chmod(temporaryPath, 0o640); err != nil {
		return ObjectInfo{}, err
	}
	if err := publishExclusive(temporaryPath, target); err != nil {
		if existing, statErr := store.Stat(ctx, stored.Key); statErr == nil && existing.Size == stored.Size && existing.SHA256 == stored.SHA256 {
			_ = store.AbortMultipart(ctx, stored)
			return existing, nil
		}
		return ObjectInfo{}, ErrConflict
	}
	info := ObjectInfo{Key: stored.Key, ContentType: stored.ContentType, Size: written, SHA256: actualSHA, ETag: quoteETag(actualSHA), LastModified: store.now().UTC(), Metadata: cloneMetadata(stored.Metadata)}
	if err := store.writeObjectMetadata(info); err != nil {
		_ = os.Remove(target)
		return ObjectInfo{}, err
	}
	if err := store.AbortMultipart(ctx, stored); err != nil {
		return ObjectInfo{}, err
	}
	return info, nil
}

func (store *LocalStore) AbortMultipart(ctx context.Context, upload MultipartUpload) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validOpaqueID(upload.ID) {
		return ErrInvalidRequest
	}
	if err := os.RemoveAll(store.multipartPath(upload.ID)); err != nil {
		return fmt.Errorf("abort local multipart upload: %w", err)
	}
	return nil
}

func (store *LocalStore) PresignGet(context.Context, string, time.Duration) (PresignedRequest, error) {
	return PresignedRequest{}, ErrUnsupported
}

func (store *LocalStore) loadUpload(provided MultipartUpload) (MultipartUpload, error) {
	if !validOpaqueID(provided.ID) {
		return MultipartUpload{}, ErrInvalidRequest
	}
	var metadata localUploadMetadata
	if err := readJSONFile(filepath.Join(store.multipartPath(provided.ID), "upload.json"), &metadata); errors.Is(err, os.ErrNotExist) {
		return MultipartUpload{}, ErrNotFound
	} else if err != nil {
		return MultipartUpload{}, fmt.Errorf("read local multipart upload: %w", err)
	}
	stored, err := validateUpload(metadata.Upload)
	if err != nil || stored.ID != provided.ID || stored.Key != provided.Key {
		return MultipartUpload{}, ErrInvalidRequest
	}
	return stored, nil
}

func (store *LocalStore) objectPath(key string, createParent bool) (string, error) {
	if err := ValidateObjectKey(key); err != nil {
		return "", err
	}
	target := filepath.Join(store.root, filepath.FromSlash(key))
	relative, err := filepath.Rel(store.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", ErrInvalidKey
	}
	if createParent {
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return "", fmt.Errorf("create local object directory: %w", err)
		}
	}
	if err := rejectSymlinkParents(store.root, filepath.Dir(target)); err != nil {
		return "", err
	}
	return target, nil
}

func (store *LocalStore) objectMetadataPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(store.root, localMetadataDirectory, hex.EncodeToString(hash[:])+".json")
}

func (store *LocalStore) multipartPath(uploadID string) string {
	hash := sha256.Sum256([]byte(uploadID))
	return filepath.Join(store.root, localMultipartDirectory, hex.EncodeToString(hash[:]))
}

func (store *LocalStore) localPartPath(uploadID string, number int32) string {
	return filepath.Join(store.multipartPath(uploadID), "parts", fmt.Sprintf("%05d.part", number))
}

func (store *LocalStore) writeObjectMetadata(info ObjectInfo) error {
	metadata := localObjectMetadata{
		Key: info.Key, ContentType: info.ContentType, Size: info.Size, SHA256: info.SHA256,
		ETag: info.ETag, LastModified: info.LastModified, Metadata: cloneMetadata(info.Metadata),
	}
	return writeJSONAtomic(store.objectMetadataPath(info.Key), metadata)
}

func objectInfoFromLocalMetadata(metadata localObjectMetadata) ObjectInfo {
	return ObjectInfo{
		Key: metadata.Key, ContentType: metadata.ContentType, Size: metadata.Size,
		SHA256: metadata.SHA256, ETag: metadata.ETag, LastModified: metadata.LastModified,
		Metadata: cloneMetadata(metadata.Metadata),
	}
}

func copyVerified(ctx context.Context, destination io.Writer, source io.Reader, expectedSize int64, expectedSHA string) (int64, string, error) {
	hash := sha256.New()
	written, err := copyWithContext(ctx, io.MultiWriter(destination, hash), io.LimitReader(source, expectedSize+1))
	if err != nil {
		return written, "", err
	}
	actualSHA := hex.EncodeToString(hash.Sum(nil))
	if written != expectedSize {
		return written, actualSHA, ErrSizeMismatch
	}
	if actualSHA != expectedSHA {
		return written, actualSHA, ErrChecksumMismatch
	}
	return written, actualSHA, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	written := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			count, writeErr := destination.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func publishExclusive(temporaryPath string, target string) error {
	if err := os.Link(temporaryPath, target); err != nil {
		return err
	}
	return os.Remove(temporaryPath)
}

func writeJSONAtomic(target string, value any) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create storage metadata directory: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode storage metadata: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".metadata-*.tmp")
	if err != nil {
		return fmt.Errorf("create storage metadata temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write storage metadata: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync storage metadata: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close storage metadata: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o640); err != nil {
		return fmt.Errorf("set storage metadata permissions: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		// Windows does not replace an existing destination atomically.
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace storage metadata: %w", err)
		}
		if retryErr := os.Rename(temporaryPath, target); retryErr != nil {
			return fmt.Errorf("publish storage metadata: %w", retryErr)
		}
	}
	return nil
}

func readJSONFile(path string, destination any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode storage metadata: %w", err)
	}
	return nil
}

func rejectSymlinkParents(root string, parent string) error {
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return ErrInvalidKey
	}
	current := root
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect local storage path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidKey
		}
	}
	return nil
}

func quoteETag(value string) string {
	return strconv.Quote(strings.Trim(value, `"`))
}
