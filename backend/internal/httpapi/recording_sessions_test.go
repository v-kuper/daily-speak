package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordingSessionChunkStorageSavesOrderedChunks(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	path, err := saveRecordingSessionChunk("session-123", 2, "webm", []byte("chunk-two"))
	if err != nil {
		t.Fatalf("expected chunk save to succeed: %v", err)
	}

	expected := filepath.Join(uploadsDir, "tmp", "recording-sessions", "session-123", "000002.webm")
	if path != expected {
		t.Fatalf("expected ordered chunk path %q, got %q", expected, path)
	}
	bytes, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("expected chunk file to exist: %v", err)
	}
	if string(bytes) != "chunk-two" {
		t.Fatalf("expected chunk bytes to be saved, got %q", string(bytes))
	}
}

func TestAssembleRecordingSessionChunksConcatenatesInOrder(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	if _, err := saveRecordingSessionChunk("session-123", 0, "webm", []byte("zero-")); err != nil {
		t.Fatalf("expected first chunk save: %v", err)
	}
	if _, err := saveRecordingSessionChunk("session-123", 2, "webm", []byte("two")); err != nil {
		t.Fatalf("expected third chunk save: %v", err)
	}
	if _, err := saveRecordingSessionChunk("session-123", 1, "webm", []byte("one-")); err != nil {
		t.Fatalf("expected second chunk save: %v", err)
	}

	outPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := assembleRecordingSessionChunks("session-123", "webm", 3, outPath); err != nil {
		t.Fatalf("expected assembly to succeed: %v", err)
	}

	bytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected assembled audio file: %v", err)
	}
	if string(bytes) != "zero-one-two" {
		t.Fatalf("expected chunks concatenated in index order, got %q", string(bytes))
	}
}

func TestAssembleRecordingSessionChunksRejectsMissingChunk(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	if _, err := saveRecordingSessionChunk("session-123", 0, "webm", []byte("zero-")); err != nil {
		t.Fatalf("expected first chunk save: %v", err)
	}
	if _, err := saveRecordingSessionChunk("session-123", 2, "webm", []byte("two")); err != nil {
		t.Fatalf("expected third chunk save: %v", err)
	}

	outPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := assembleRecordingSessionChunks("session-123", "webm", 3, outPath); err == nil {
		t.Fatal("expected assembly to fail when a chunk is missing")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected missing chunk assembly to avoid final file, got stat error %v", err)
	}
}

func TestReadMultipartChunkRequestParsesChunkIndexAndAudio(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("chunkIndex", "7"); err != nil {
		t.Fatalf("expected chunk index field: %v", err)
	}
	file, err := writer.CreateFormFile("audio", "chunk.webm")
	if err != nil {
		t.Fatalf("expected audio part: %v", err)
	}
	if _, err := file.Write([]byte("chunk-seven")); err != nil {
		t.Fatalf("expected audio write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("expected multipart close: %v", err)
	}
	request := httptest.NewRequest("POST", "/api/recording-sessions/session-123/chunks", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	chunk, err := readMultipartChunkRequest(request)
	if err != nil {
		t.Fatalf("expected multipart chunk parse to succeed: %v", err)
	}
	if chunk.Index != 7 {
		t.Fatalf("expected chunk index 7, got %d", chunk.Index)
	}
	if chunk.Extension != "webm" {
		t.Fatalf("expected webm extension, got %q", chunk.Extension)
	}
	if string(chunk.Bytes) != "chunk-seven" {
		t.Fatalf("expected chunk bytes, got %q", string(chunk.Bytes))
	}
}

func TestReadMultipartChunkRequestPrefersContentTypeForExtension(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("chunkIndex", "1"); err != nil {
		t.Fatalf("expected chunk index field: %v", err)
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="audio"; filename="chunk.webm"`},
		"Content-Type":        {"audio/mp4"},
	})
	if err != nil {
		t.Fatalf("expected audio part: %v", err)
	}
	if _, err := part.Write([]byte("chunk-one")); err != nil {
		t.Fatalf("expected audio write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("expected multipart close: %v", err)
	}
	request := httptest.NewRequest("POST", "/api/recording-sessions/session-123/chunks", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	chunk, err := readMultipartChunkRequest(request)
	if err != nil {
		t.Fatalf("expected multipart chunk parse to succeed: %v", err)
	}
	if chunk.Extension != "m4a" {
		t.Fatalf("expected m4a extension from content type, got %q", chunk.Extension)
	}
}
