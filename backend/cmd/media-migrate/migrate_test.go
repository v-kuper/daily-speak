package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/storage"
)

func TestMigratorDryRunHashesSourcesWithoutWritingTargetOrDatabase(t *testing.T) {
	body := []byte("legacy recording")
	repository := &fakeAssetRepository{assets: []legacyAsset{{
		ID: "legacy-media-one", OwnerPrincipalID: "user-1", Purpose: "recording_audio",
		ObjectKey: "recordings/user-1/one.webm", ContentType: "audio/webm",
	}}}
	source := &memorySource{objects: map[string][]byte{"recordings/user-1/one.webm": body}}
	target := newMemoryTarget(storage.BackendS3)

	summary, err := (migrator{repository: repository, source: source, target: target}).Run(context.Background(), migrationOptions{
		Apply: false, Limit: 100, Bucket: "private-media",
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !summary.DryRun || summary.Scanned != 1 || summary.Planned != 1 || summary.Bytes != int64(len(body)) || summary.Copied != 0 || summary.Updated != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if target.puts != 0 || len(repository.switches) != 0 {
		t.Fatalf("dry-run mutated state: puts=%d switches=%d", target.puts, len(repository.switches))
	}
}

func TestMigratorApplyCopiesVerifiesThenAtomicallySwitchesAsset(t *testing.T) {
	body := []byte("legacy recording")
	repository := &fakeAssetRepository{assets: []legacyAsset{{
		ID: "legacy-media-one", OwnerPrincipalID: "user-1", Purpose: "recording_audio",
		ObjectKey: "recordings/user-1/one.webm", ContentType: "audio/webm",
	}}}
	target := newMemoryTarget(storage.BackendS3)
	summary, err := (migrator{
		repository: repository,
		source:     &memorySource{objects: map[string][]byte{"recordings/user-1/one.webm": body}},
		target:     target,
	}).Run(context.Background(), migrationOptions{Apply: true, Limit: 100, Bucket: "private-media"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if summary.Copied != 1 || summary.Updated != 1 || summary.Failed != 0 || len(repository.switches) != 1 {
		t.Fatalf("unexpected summary=%#v switches=%#v", summary, repository.switches)
	}
	switchRequest := repository.switches[0]
	if switchRequest.TargetBucket != "private-media" || switchRequest.SourceObjectKey != "recordings/user-1/one.webm" || switchRequest.Size != int64(len(body)) || switchRequest.SHA256 != testChecksum(body) {
		t.Fatalf("unexpected atomic switch request: %#v", switchRequest)
	}
	if !strings.HasPrefix(switchRequest.TargetObjectKey, "migrated/") || !strings.HasSuffix(switchRequest.TargetObjectKey, ".webm") {
		t.Fatalf("unexpected target key %q", switchRequest.TargetObjectKey)
	}
}

func TestMigratorApplyIsResumableWhenTargetObjectAlreadyExists(t *testing.T) {
	body := []byte("copied before process crash")
	asset := legacyAsset{ID: "legacy-media-resume", OwnerPrincipalID: "user-1", Purpose: "recording_audio", ObjectKey: "recordings/user-1/resume.webm", ContentType: "audio/webm"}
	targetKey, err := migrationTargetKey(asset)
	if err != nil {
		t.Fatal(err)
	}
	target := newMemoryTarget(storage.BackendS3)
	target.objects[targetKey] = memoryObject{body: body, contentType: asset.ContentType, checksum: testChecksum(body), etag: `"existing"`}
	repository := &fakeAssetRepository{assets: []legacyAsset{asset}, switchResult: switchAssetResult{Already: true}}

	summary, err := (migrator{
		repository: repository,
		source:     &memorySource{objects: map[string][]byte{asset.ObjectKey: body}},
		target:     target,
	}).Run(context.Background(), migrationOptions{Apply: true, Limit: 100, Bucket: "private-media"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.AlreadyApplied != 1 || summary.Failed != 0 {
		t.Fatalf("resume summary = %#v", summary)
	}
}

func TestMigratorDoesNotSwitchDatabaseWhenTargetVerificationFails(t *testing.T) {
	body := []byte("legacy recording")
	asset := legacyAsset{ID: "legacy-media-bad", OwnerPrincipalID: "user-1", Purpose: "recording_audio", ObjectKey: "recordings/user-1/bad.webm", ContentType: "audio/webm"}
	target := newMemoryTarget(storage.BackendS3)
	target.corruptStat = true
	repository := &fakeAssetRepository{assets: []legacyAsset{asset}}

	summary, err := (migrator{
		repository: repository,
		source:     &memorySource{objects: map[string][]byte{asset.ObjectKey: body}},
		target:     target,
	}).Run(context.Background(), migrationOptions{Apply: true, Limit: 100, Bucket: "private-media"})
	if err == nil || summary.Failed != 1 {
		t.Fatalf("verification failure: summary=%#v err=%v", summary, err)
	}
	if len(repository.switches) != 0 {
		t.Fatal("database switched before target verification")
	}
}

func TestMigratorDoesNotTrustTargetChecksumMetadataWithoutReadingBytes(t *testing.T) {
	body := []byte("legacy recording")
	asset := legacyAsset{ID: "legacy-media-corrupt-body", OwnerPrincipalID: "user-1", Purpose: "recording_audio", ObjectKey: "recordings/user-1/corrupt.webm", ContentType: "audio/webm"}
	target := newMemoryTarget(storage.BackendS3)
	target.corruptOpen = true
	repository := &fakeAssetRepository{assets: []legacyAsset{asset}}

	summary, err := (migrator{
		repository: repository,
		source:     &memorySource{objects: map[string][]byte{asset.ObjectKey: body}},
		target:     target,
	}).Run(context.Background(), migrationOptions{Apply: true, Limit: 100, Bucket: "private-media"})
	if err == nil || summary.Failed != 1 || len(repository.switches) != 0 {
		t.Fatalf("corrupt target bytes were trusted: summary=%#v err=%v", summary, err)
	}
}

func TestMigratorRejectsUnexpectedLegacyMetadataBeforeCopy(t *testing.T) {
	body := []byte("actual")
	wrongSize := int64(99)
	asset := legacyAsset{
		ID: "legacy-media-mismatch", OwnerPrincipalID: "user-1", Purpose: "recording_audio",
		ObjectKey: "recordings/user-1/mismatch.webm", ContentType: "audio/webm", ExpectedSizeBytes: &wrongSize,
	}
	target := newMemoryTarget(storage.BackendS3)
	repository := &fakeAssetRepository{assets: []legacyAsset{asset}}
	summary, err := (migrator{
		repository: repository, source: &memorySource{objects: map[string][]byte{asset.ObjectKey: body}}, target: target,
	}).Run(context.Background(), migrationOptions{Apply: true, Limit: 100, Bucket: "private-media"})
	if err == nil || summary.Failed != 1 || target.puts != 0 || len(repository.switches) != 0 {
		t.Fatalf("metadata mismatch was not contained: summary=%#v err=%v puts=%d switches=%d", summary, err, target.puts, len(repository.switches))
	}
}

func TestMigratorRequiresExplicitS3Target(t *testing.T) {
	repository := &fakeAssetRepository{}
	_, err := (migrator{repository: repository, source: &memorySource{}, target: newMemoryTarget(storage.BackendLocal)}).Run(context.Background(), migrationOptions{Limit: 100, Bucket: "private-media"})
	if err == nil || repository.listCalls != 0 {
		t.Fatalf("local target should fail before reading DB: err=%v listCalls=%d", err, repository.listCalls)
	}
}

func TestRunDefaultsToSafeLocalNoOpWithoutDatabase(t *testing.T) {
	clearMediaMigrationEnv(t)
	t.Setenv("UPLOADS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "MEDIA_STORAGE_DRIVER=s3") {
		t.Fatalf("default local run error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestMigrationTargetKeyIsDeterministicAndDoesNotReuseLegacyPath(t *testing.T) {
	asset := legacyAsset{ID: "legacy-media-one", ObjectKey: "recordings/user/private-name.webm", ContentType: "audio/webm"}
	first, err := migrationTargetKey(asset)
	if err != nil {
		t.Fatal(err)
	}
	second, err := migrationTargetKey(asset)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || strings.Contains(first, "private-name") || strings.Contains(first, "recordings/user") {
		t.Fatalf("unsafe/non-deterministic target keys: %q %q", first, second)
	}
	if err := storage.ValidateObjectKey(first); err != nil {
		t.Fatalf("target key is invalid: %v", err)
	}
}

func TestFilesystemSourceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.webm")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "recordings"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "recordings", "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source, err := newFilesystemSource(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Open(context.Background(), "recordings/escape/secret.webm"); !errors.Is(err, storage.ErrInvalidKey) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

type fakeAssetRepository struct {
	assets       []legacyAsset
	switches     []switchAssetRequest
	switchResult switchAssetResult
	listCalls    int
}

func (repository *fakeAssetRepository) ListLegacyLocal(_ context.Context, limit int, assetID string) ([]legacyAsset, error) {
	repository.listCalls++
	result := make([]legacyAsset, 0, len(repository.assets))
	for _, asset := range repository.assets {
		if assetID != "" && asset.ID != assetID {
			continue
		}
		result = append(result, asset)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (repository *fakeAssetRepository) SwitchToS3(_ context.Context, request switchAssetRequest) (switchAssetResult, error) {
	repository.switches = append(repository.switches, request)
	if repository.switchResult == (switchAssetResult{}) {
		return switchAssetResult{Updated: true}, nil
	}
	return repository.switchResult, nil
}

type memorySource struct {
	objects map[string][]byte
}

func (source *memorySource) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := source.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type memoryObject struct {
	body        []byte
	contentType string
	checksum    string
	etag        string
}

type memoryTarget struct {
	backend     string
	objects     map[string]memoryObject
	puts        int
	corruptStat bool
	corruptOpen bool
}

func newMemoryTarget(backend string) *memoryTarget {
	return &memoryTarget{backend: backend, objects: map[string]memoryObject{}}
}

func (target *memoryTarget) Backend() string { return target.backend }

func (target *memoryTarget) Put(_ context.Context, request storage.PutRequest, reader io.Reader) (storage.ObjectInfo, error) {
	target.puts++
	body, err := io.ReadAll(reader)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if int64(len(body)) != request.Size {
		return storage.ObjectInfo{}, storage.ErrSizeMismatch
	}
	if testChecksum(body) != request.SHA256 {
		return storage.ObjectInfo{}, storage.ErrChecksumMismatch
	}
	if existing, ok := target.objects[request.Key]; ok {
		if existing.checksum != request.SHA256 || int64(len(existing.body)) != request.Size {
			return storage.ObjectInfo{}, storage.ErrConflict
		}
		return target.objectInfo(request.Key, existing), nil
	}
	object := memoryObject{body: append([]byte(nil), body...), contentType: request.ContentType, checksum: request.SHA256, etag: `"etag"`}
	target.objects[request.Key] = object
	return target.objectInfo(request.Key, object), nil
}

func (target *memoryTarget) Stat(_ context.Context, key string) (storage.ObjectInfo, error) {
	object, ok := target.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	info := target.objectInfo(key, object)
	if target.corruptStat {
		info.SHA256 = strings.Repeat("0", 64)
	}
	return info, nil
}

func (target *memoryTarget) Open(_ context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	object, ok := target.objects[key]
	if !ok {
		return nil, storage.ObjectInfo{}, storage.ErrNotFound
	}
	body := object.body
	if target.corruptOpen {
		body = append(append([]byte(nil), body...), byte('!'))
	}
	return io.NopCloser(bytes.NewReader(body)), target.objectInfo(key, object), nil
}

func (target *memoryTarget) objectInfo(key string, object memoryObject) storage.ObjectInfo {
	return storage.ObjectInfo{Key: key, ContentType: object.contentType, Size: int64(len(object.body)), SHA256: object.checksum, ETag: object.etag}
}

func testChecksum(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func clearMediaMigrationEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MEDIA_STORAGE_DRIVER", "MEDIA_LOCAL_DIR", "UPLOADS_DIR",
		"MEDIA_S3_BUCKET", "MEDIA_S3_REGION", "MEDIA_S3_ENDPOINT",
		"MEDIA_S3_ACCESS_KEY_ID", "MEDIA_S3_SECRET_ACCESS_KEY", "MEDIA_S3_SESSION_TOKEN",
		"MEDIA_S3_FORCE_PATH_STYLE", "MEDIA_STORAGE_KEY_PREFIX", "MEDIA_UPLOAD_URL_TTL",
		"MEDIA_MULTIPART_PART_SIZE_BYTES", "DATABASE_URL", "DATABASE_SSL",
	} {
		t.Setenv(name, "")
	}
}
