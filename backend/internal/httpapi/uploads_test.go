package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"daily-speaking-practice/backend/internal/domain"
)

func TestSaveAudioFileUsesConfiguredUploadsDir(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	audio := &domain.ParsedAudioDataURL{
		Base64:    base64.StdEncoding.EncodeToString([]byte("audio-bytes")),
		Extension: "webm",
	}

	saved, err := saveAudioFile("recordings", "user-123", "recording-456", audio)
	if err != nil {
		t.Fatalf("expected audio save to succeed: %v", err)
	}

	expectedPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if saved.absolutePath != expectedPath {
		t.Fatalf("expected audio to be saved in configured uploads dir, got %q", saved.absolutePath)
	}
	if saved.publicURL != "/uploads/recordings/user-123/recording-456.webm" {
		t.Fatalf("unexpected public URL %q", saved.publicURL)
	}
	if bytes, err := os.ReadFile(expectedPath); err != nil || string(bytes) != "audio-bytes" {
		t.Fatalf("expected saved audio bytes, got %q with error %v", string(bytes), err)
	}
}

func TestHandlerServesUploadsFromConfiguredDir(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	audioPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("expected test uploads directory: %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("expected test audio file: %v", err)
	}

	handler := NewServer(Config{}).Handler()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/uploads/recordings/user-123/recording-456.webm", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "audio-bytes" {
		t.Fatalf("expected uploaded audio bytes, got %q", recorder.Body.String())
	}
}
