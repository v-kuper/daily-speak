package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/storage"
)

func TestLocalPresignedPlansReceiveBoundedFutureExpiry(t *testing.T) {
	now := time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)
	resource := testUploadResource(now)
	repository := &stubRepository{upload: resource, asset: resource.Asset}
	service := NewService(repository, stubLocalStore{}, Config{
		Now: func() time.Time { return now }, SignedRequestTTL: 10 * time.Minute,
	})
	parts, err := service.PresignParts(context.Background(), resource.Asset.OwnerPrincipalID, resource.Upload.ID, []PartDescriptor{{
		PartNumber: 1, SizeBytes: resource.Asset.ExpectedSizeBytes,
		ChecksumSHA256: strings.Repeat("b", 64),
	}})
	if err != nil {
		t.Fatalf("presign local part: %v", err)
	}
	if len(parts) != 1 || !parts[0].Local || parts[0].Request.Method != http.MethodPut || !parts[0].Request.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected local part plan: %+v", parts)
	}
	if values := parts[0].Request.Headers["Content-Type"]; len(values) != 1 || values[0] != resource.Asset.ContentType {
		t.Fatalf("content type header = %#v", values)
	}
	download, err := service.Download(context.Background(), DownloadInput{
		OwnerPrincipalID: resource.Asset.OwnerPrincipalID, OwnerKind: "user", AssetID: resource.Asset.ID,
	})
	if err != nil {
		t.Fatalf("presign local download: %v", err)
	}
	if !download.Local || download.Request.Method != http.MethodGet || !download.Request.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected local download plan: %+v", download)
	}
}

func TestDownloadPolicyRequiresAnAccountBeforeRepositoryAccess(t *testing.T) {
	service := NewService(&stubRepository{}, stubLocalStore{}, Config{})
	_, err := service.Download(context.Background(), DownloadInput{
		OwnerPrincipalID: "guest-id", OwnerKind: "guest", AssetID: "asset-id",
	})
	if !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("download error = %v, want account required", err)
	}
}

func TestGuestPreviewPurposeIsInternalOnly(t *testing.T) {
	asset := Asset{Purpose: PurposeGuestPreviewAudio}
	if got := asset.ClientPurpose(); got != PurposeRecordingAudio {
		t.Fatalf("client purpose = %q", got)
	}
}

func TestStorageResponsesAreMappedToMediaModels(t *testing.T) {
	source := storage.PresignedRequest{
		Method: http.MethodPut, URL: "https://storage.example/upload",
		Headers: http.Header{"Content-Type": {"audio/webm"}},
	}
	request := signedRequestFromStorage(source)
	if request.Method != http.MethodPut || request.URL != "https://storage.example/upload" {
		t.Fatalf("signed request = %#v", request)
	}
	request.Headers["Content-Type"][0] = "changed"
	if source.Headers.Get("Content-Type") != "audio/webm" {
		t.Fatal("media mapping mutated the storage response")
	}

	part := uploadedPartFromStorage(storage.PartInfo{Number: 2, Size: 1024, ETag: "etag", SHA256: "checksum"})
	if part.PartNumber != 2 || part.SizeBytes != 1024 || part.ChecksumSHA256 != "checksum" {
		t.Fatalf("uploaded part = %#v", part)
	}
}

func TestCreateInputValidationRejectsUnsupportedOrOversizedMedia(t *testing.T) {
	base := CreateUploadInput{
		OwnerPrincipalID: "principal-1", OwnerKind: "user", SessionID: "session-1",
		IdempotencyKey: "request-1234", Purpose: PurposeRecordingAudio,
		ContentType: "audio/webm", SizeBytes: 1024,
		ChecksumSHA256: strings.Repeat("a", 64),
	}
	if _, err := validateCreateInput(base); err != nil {
		t.Fatalf("valid audio rejected: %v", err)
	}
	invalidMIME := base
	invalidMIME.ContentType = "application/octet-stream"
	if _, err := validateCreateInput(invalidMIME); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("unsupported MIME error = %v", err)
	}
	invalidChecksum := base
	invalidChecksum.ChecksumSHA256 = "not-a-checksum"
	if _, err := validateCreateInput(invalidChecksum); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("invalid checksum error = %v", err)
	}
}

func TestGuestPreviewUploadUsesTighterAudioLimit(t *testing.T) {
	input := CreateUploadInput{
		OwnerPrincipalID: "guest-1", OwnerKind: "guest", SessionID: "session-1",
		IdempotencyKey: "request-1234", Purpose: PurposeGuestPreviewAudio,
		ContentType: "audio/webm", SizeBytes: 10 * 1024 * 1024,
		ChecksumSHA256: strings.Repeat("a", 64),
	}
	if _, err := validateCreateInput(input); err != nil {
		t.Fatalf("guest upload at limit: %v", err)
	}
	input.SizeBytes++
	if _, err := validateCreateInput(input); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("guest upload above limit error = %v", err)
	}
}

func TestCreateUploadPolicyMapsOnlyGuestAudioToPreviewPurpose(t *testing.T) {
	guestAudio, err := applyCreateOwnerPolicy(normalizeCreateInput(CreateUploadInput{OwnerKind: " guest ", Purpose: PurposeRecordingAudio}))
	if err != nil {
		t.Fatalf("guest audio policy: %v", err)
	}
	if guestAudio.Purpose != PurposeGuestPreviewAudio {
		t.Fatalf("guest audio purpose = %q", guestAudio.Purpose)
	}

	if _, err := applyCreateOwnerPolicy(normalizeCreateInput(CreateUploadInput{OwnerKind: "guest", Purpose: PurposeRecordingPhoto})); !errors.Is(err, ErrGuestRestricted) {
		t.Fatalf("guest photo policy error = %v", err)
	}

	userAudio, err := applyCreateOwnerPolicy(normalizeCreateInput(CreateUploadInput{OwnerKind: "user", Purpose: PurposeRecordingAudio}))
	if err != nil {
		t.Fatalf("user audio policy: %v", err)
	}
	if userAudio.Purpose != PurposeRecordingAudio {
		t.Fatalf("user audio purpose = %q", userAudio.Purpose)
	}

	if _, err := applyCreateOwnerPolicy(normalizeCreateInput(CreateUploadInput{OwnerKind: "user", Purpose: PurposeGuestPreviewAudio})); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("public internal purpose error = %v", err)
	}
}

func TestCompletingUploadFencesConcurrentAbort(t *testing.T) {
	now := time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)
	resource := testUploadResource(now)
	resource.Asset.State = "uploading"
	repository := &statefulRepository{upload: resource}
	store := &blockingCompletionStore{started: make(chan struct{}), release: make(chan struct{})}
	service := NewService(repository, store, Config{Now: func() time.Time { return now }})
	completed := []CompletedPart{{PartNumber: 1, ETag: "etag", ChecksumSHA256: strings.Repeat("a", 64)}}

	result := make(chan error, 1)
	go func() {
		_, err := service.CompleteUpload(context.Background(), resource.Asset.OwnerPrincipalID, resource.Upload.ID, completed)
		result <- err
	}()
	<-store.started
	if _, err := service.AbortUpload(context.Background(), resource.Asset.OwnerPrincipalID, resource.Upload.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent abort error = %v, want conflict", err)
	}
	close(store.release)
	if err := <-result; err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if store.abortCalls != 0 || store.deleteCalls != 0 {
		t.Fatalf("completion was disrupted: aborts=%d deletes=%d", store.abortCalls, store.deleteCalls)
	}
}

func TestInvalidCompletionDoesNotClaimUpload(t *testing.T) {
	now := time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)
	resource := testUploadResource(now)
	repository := &statefulRepository{upload: resource}
	service := NewService(repository, stubLocalStore{}, Config{Now: func() time.Time { return now }})
	if _, err := service.CompleteUpload(context.Background(), resource.Asset.OwnerPrincipalID, resource.Upload.ID, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid completion error = %v", err)
	}
	current, _ := repository.GetUploadByID(context.Background(), resource.Upload.ID)
	if current.Upload.State != "uploading" {
		t.Fatalf("invalid completion claimed upload state: %s", current.Upload.State)
	}
}

func testUploadResource(now time.Time) UploadResource {
	expires := now.Add(time.Hour)
	return UploadResource{
		Asset: Asset{
			ID: "asset-1", OwnerPrincipalID: "principal-1", Purpose: PurposeRecordingAudio,
			State: "ready", StorageDriver: storage.BackendLocal,
			ObjectKey:   "v1/principal-1/recording_audio/audio.webm",
			ContentType: "audio/webm", ExpectedSizeBytes: 1024,
			ExpectedChecksumSHA256: strings.Repeat("a", 64), CreatedAt: now, UpdatedAt: now,
		},
		Upload: Upload{
			ID: "upload-1", AssetID: "asset-1", ProviderUploadID: "provider-1",
			State: "uploading", PartSizeBytes: defaultPartSizeBytes, PartCount: 1,
			ExpiresAt: expires, CreatedBySessionID: "session-1", IdempotencyKey: "request-1234",
			CreatedAt: now, UpdatedAt: now,
		},
	}
}

type stubRepository struct {
	upload UploadResource
	asset  Asset
}

func (repository *stubRepository) FindByIdempotency(context.Context, string, string) (UploadResource, error) {
	return UploadResource{}, ErrNotFound
}
func (repository *stubRepository) InsertUpload(context.Context, Asset, Upload) error { return nil }
func (repository *stubRepository) GetUpload(context.Context, string, string) (UploadResource, error) {
	return repository.upload, nil
}
func (repository *stubRepository) GetUploadByID(context.Context, string) (UploadResource, error) {
	return repository.upload, nil
}
func (repository *stubRepository) UpsertPart(context.Context, Part) error        { return nil }
func (repository *stubRepository) ClaimCompleting(context.Context, string) error { return nil }
func (repository *stubRepository) ClaimAborting(context.Context, string, *time.Time) error {
	return nil
}
func (repository *stubRepository) MarkCompleted(context.Context, string, VerifiedObject, time.Time) (UploadResource, error) {
	return repository.upload, nil
}
func (repository *stubRepository) MarkAborted(context.Context, string, time.Time) (UploadResource, error) {
	return repository.upload, nil
}
func (repository *stubRepository) GetReadyAsset(context.Context, string, string) (Asset, error) {
	return repository.asset, nil
}
func (repository *stubRepository) GetReadyAssetByID(context.Context, string) (Asset, error) {
	return repository.asset, nil
}

type stubLocalStore struct{}

func (stubLocalStore) Backend() string { return storage.BackendLocal }
func (stubLocalStore) Put(context.Context, storage.PutRequest, io.Reader) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrUnsupported
}
func (stubLocalStore) Open(context.Context, string) (io.ReadCloser, storage.ObjectInfo, error) {
	return nil, storage.ObjectInfo{}, storage.ErrUnsupported
}
func (stubLocalStore) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrUnsupported
}
func (stubLocalStore) Delete(context.Context, string) error { return storage.ErrUnsupported }
func (stubLocalStore) CreateMultipart(context.Context, storage.MultipartRequest) (storage.MultipartUpload, error) {
	return storage.MultipartUpload{}, storage.ErrUnsupported
}
func (stubLocalStore) PutPart(context.Context, storage.MultipartUpload, storage.PartRequest, io.Reader) (storage.PartInfo, error) {
	return storage.PartInfo{}, storage.ErrUnsupported
}
func (stubLocalStore) PresignUploadPart(context.Context, storage.MultipartUpload, storage.PartRequest, time.Duration) (storage.PresignedRequest, error) {
	return storage.PresignedRequest{}, storage.ErrUnsupported
}
func (stubLocalStore) ListParts(context.Context, storage.MultipartUpload) ([]storage.PartInfo, error) {
	return nil, storage.ErrUnsupported
}
func (stubLocalStore) CompleteMultipart(context.Context, storage.MultipartUpload, []storage.CompletedPart) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrUnsupported
}
func (stubLocalStore) AbortMultipart(context.Context, storage.MultipartUpload) error {
	return storage.ErrUnsupported
}
func (stubLocalStore) PresignGet(context.Context, string, time.Duration) (storage.PresignedRequest, error) {
	return storage.PresignedRequest{}, storage.ErrUnsupported
}

type statefulRepository struct {
	mu     sync.Mutex
	upload UploadResource
}

func (repository *statefulRepository) FindByIdempotency(context.Context, string, string) (UploadResource, error) {
	return UploadResource{}, ErrNotFound
}
func (repository *statefulRepository) InsertUpload(context.Context, Asset, Upload) error { return nil }
func (repository *statefulRepository) GetUpload(context.Context, string, string) (UploadResource, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.upload, nil
}
func (repository *statefulRepository) GetUploadByID(context.Context, string) (UploadResource, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.upload, nil
}
func (repository *statefulRepository) UpsertPart(context.Context, Part) error { return nil }
func (repository *statefulRepository) ClaimCompleting(context.Context, string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.upload.Upload.State != "pending" && repository.upload.Upload.State != "uploading" {
		return ErrConflict
	}
	repository.upload.Upload.State = "completing"
	return nil
}
func (repository *statefulRepository) ClaimAborting(_ context.Context, _ string, staleBefore *time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.upload.Upload.State == "completing" && staleBefore == nil {
		return ErrConflict
	}
	repository.upload.Upload.State = "aborting"
	return nil
}
func (repository *statefulRepository) MarkCompleted(_ context.Context, _ string, info VerifiedObject, now time.Time) (UploadResource, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.upload.Upload.State != "completing" {
		return UploadResource{}, ErrConflict
	}
	repository.upload.Upload.State = "completed"
	repository.upload.Asset.State = "ready"
	repository.upload.Asset.VerifiedSizeBytes = &info.SizeBytes
	repository.upload.Asset.VerifiedChecksumSHA256 = &info.ChecksumSHA256
	repository.upload.Upload.CompletedAt = &now
	return repository.upload, nil
}
func (repository *statefulRepository) MarkAborted(context.Context, string, time.Time) (UploadResource, error) {
	return UploadResource{}, ErrConflict
}
func (repository *statefulRepository) GetReadyAsset(context.Context, string, string) (Asset, error) {
	return Asset{}, ErrNotFound
}
func (repository *statefulRepository) GetReadyAssetByID(context.Context, string) (Asset, error) {
	return Asset{}, ErrNotFound
}

type blockingCompletionStore struct {
	stubLocalStore
	started     chan struct{}
	release     chan struct{}
	abortCalls  int
	deleteCalls int
}

func (store *blockingCompletionStore) CompleteMultipart(_ context.Context, upload storage.MultipartUpload, _ []storage.CompletedPart) (storage.ObjectInfo, error) {
	close(store.started)
	<-store.release
	return storage.ObjectInfo{Key: upload.Key, Size: upload.Size, SHA256: upload.SHA256, ETag: "etag"}, nil
}
func (store *blockingCompletionStore) AbortMultipart(context.Context, storage.MultipartUpload) error {
	store.abortCalls++
	return nil
}
func (store *blockingCompletionStore) Delete(context.Context, string) error {
	store.deleteCalls++
	return nil
}
