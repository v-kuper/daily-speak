package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorePutOpenStatDeleteContract(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("recording-audio")
	request := testPutRequest("v1/user/recording/audio.webm", "audio/webm", body)
	request.Metadata = map[string]string{"purpose": "recording"}

	created, err := store.Put(ctx, request, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if created.Size != int64(len(body)) || created.SHA256 != request.SHA256 || created.ContentType != "audio/webm" {
		t.Fatalf("unexpected object info: %#v", created)
	}

	stat, err := store.Stat(ctx, request.Key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.SHA256 != request.SHA256 || stat.Metadata["purpose"] != "recording" {
		t.Fatalf("unexpected stat: %#v", stat)
	}
	reader, opened, err := store.Open(ctx, request.Key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	read, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil || !bytes.Equal(read, body) || opened.SHA256 != request.SHA256 {
		t.Fatalf("opened object = %q %#v, err=%v", read, opened, err)
	}

	if _, err := store.Put(ctx, request, bytes.NewReader(body)); err != nil {
		t.Fatalf("idempotent Put: %v", err)
	}
	conflictBody := []byte("different")
	if _, err := store.Put(ctx, testPutRequest(request.Key, "audio/webm", conflictBody), bytes.NewReader(conflictBody)); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Put error = %v", err)
	}
	if err := store.Delete(ctx, request.Key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := store.Delete(ctx, request.Key); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if _, err := store.Stat(ctx, request.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat after delete = %v", err)
	}
}

func TestLocalStoreRejectsSizeChecksumAndTraversalWithoutPublishing(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("audio")
	request := testPutRequest("v1/user/recording/audio.webm", "audio/webm", body)
	request.Size++
	if _, err := store.Put(context.Background(), request, bytes.NewReader(body)); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("size mismatch error = %v", err)
	}
	request = testPutRequest("v1/user/recording/audio.webm", "audio/webm", body)
	request.SHA256 = checksum([]byte("wrong"))
	if _, err := store.Put(context.Background(), request, bytes.NewReader(body)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	if _, err := store.Put(context.Background(), PutRequest{Key: "../escape", ContentType: "audio/webm", Size: int64(len(body)), SHA256: checksum(body)}, bytes.NewReader(body)); !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("traversal error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "v1", "user", "recording", "audio.webm")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid objects must not be published: %v", err)
	}
}

func TestLocalStoreReadsAndIndexesLegacyObject(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "recordings", "user", "legacy.webm")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o750); err != nil {
		t.Fatal(err)
	}
	body := []byte{0x1a, 0x45, 0xdf, 0xa3}
	if err := os.WriteFile(legacyPath, body, 0o640); err != nil {
		t.Fatal(err)
	}
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Stat(context.Background(), "recordings/user/legacy.webm")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(body)) || info.SHA256 != checksum(body) {
		t.Fatalf("legacy info = %#v", info)
	}
}

func TestLocalStoreMultipartLifecycleAndRetry(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := []byte("first-")
	second := []byte("second")
	whole := append(append([]byte(nil), first...), second...)
	upload, err := store.CreateMultipart(ctx, testPutRequest("v1/user/recording/multipart.webm", "audio/webm", whole))
	if err != nil {
		t.Fatalf("CreateMultipart: %v", err)
	}
	partTwo, err := store.PutPart(ctx, upload, PartRequest{Number: 2, Size: int64(len(second)), SHA256: checksum(second)}, bytes.NewReader(second))
	if err != nil {
		t.Fatalf("PutPart(2): %v", err)
	}
	partOne, err := store.PutPart(ctx, upload, PartRequest{Number: 1, Size: int64(len(first)), SHA256: checksum(first)}, bytes.NewReader(first))
	if err != nil {
		t.Fatalf("PutPart(1): %v", err)
	}
	if _, err := store.PutPart(ctx, upload, PartRequest{Number: 1, Size: int64(len(first)), SHA256: checksum(first)}, bytes.NewReader(first)); err != nil {
		t.Fatalf("idempotent PutPart: %v", err)
	}
	parts, err := store.ListParts(ctx, upload)
	if err != nil {
		t.Fatalf("ListParts: %v", err)
	}
	if len(parts) != 2 || parts[0].Number != 1 || parts[1].Number != 2 {
		t.Fatalf("parts = %#v", parts)
	}
	completed := []CompletedPart{{Number: 2, ETag: partTwo.ETag, SHA256: partTwo.SHA256}, {Number: 1, ETag: partOne.ETag, SHA256: partOne.SHA256}}
	object, err := store.CompleteMultipart(ctx, upload, completed)
	if err != nil {
		t.Fatalf("CompleteMultipart: %v", err)
	}
	if object.SHA256 != checksum(whole) || object.Size != int64(len(whole)) {
		t.Fatalf("completed object = %#v", object)
	}
	if retried, err := store.CompleteMultipart(ctx, upload, completed); err != nil || retried.SHA256 != object.SHA256 {
		t.Fatalf("idempotent CompleteMultipart = %#v, %v", retried, err)
	}
	if err := store.AbortMultipart(ctx, upload); err != nil {
		t.Fatalf("idempotent AbortMultipart: %v", err)
	}
}

func TestLocalStoreMultipartRefusesUnverifiedParts(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := []byte("part")
	upload, err := store.CreateMultipart(ctx, testPutRequest("v1/user/recording/bad.webm", "audio/webm", body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutPart(ctx, upload, PartRequest{Number: 1, Size: int64(len(body)), SHA256: checksum([]byte("other"))}, bytes.NewReader(body)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("PutPart checksum error = %v", err)
	}
	if _, err := store.CompleteMultipart(ctx, upload, []CompletedPart{{Number: 1, ETag: "missing"}}); err == nil {
		t.Fatal("completion without a verified stored part must fail")
	}
}

func testPutRequest(key string, contentType string, body []byte) PutRequest {
	return PutRequest{Key: key, ContentType: contentType, Size: int64(len(body)), SHA256: checksum(body)}
}

func checksum(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}
