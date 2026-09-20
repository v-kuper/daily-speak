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

func TestShadowingUploadRequiresAuthentication(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	audioPath := filepath.Join(uploadsDir, "shadowing", "user-123", "recording-456.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/uploads/shadowing/user-123/recording-456.mp3", nil)
	NewServer(Config{}).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestShadowingUploadRejectsDirectoryListing(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	if err := os.MkdirAll(filepath.Join(uploadsDir, "shadowing", "user-123"), 0o755); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/uploads/shadowing/", nil)
	NewServer(Config{}).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestStoredUploadPathRejectsTraversalOutsideConfiguredDir(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	if _, err := storedUploadPath("/uploads/../private.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestStoredUploadPathAcceptsShadowingAndRejectsUnsafeShapes(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	got, err := storedUploadPath("/uploads/shadowing/user-1/recording-1.mp3")
	if err != nil {
		t.Fatalf("expected shadowing path to be accepted: %v", err)
	}
	want := filepath.Join(uploadsDir, "shadowing", "user-1", "recording-1.mp3")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	for _, value := range []string{
		"/uploads/shadowing/../../secret",
		"/uploads/shadowing/user-1/nested/recording-1.mp3",
	} {
		if _, err := storedUploadPath(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestRemoveStoredUploadFilesDeletesExistingFilesAndIgnoresMissingFiles(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	audioPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatalf("expected test uploads directory: %v", err)
	}
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatalf("expected test audio file: %v", err)
	}

	err := removeStoredUploadFiles([]string{
		"/uploads/recordings/user-123/recording-456.webm",
		"/uploads/feed-replies/user-123/missing.webm",
	})

	if err != nil {
		t.Fatalf("expected file cleanup to succeed: %v", err)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Fatalf("expected recording audio to be removed, got %v", err)
	}
}
