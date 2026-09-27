package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

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
