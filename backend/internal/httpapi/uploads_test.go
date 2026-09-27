package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	handler := newTestServer(Config{}).Handler()
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

func TestHandlerDoesNotExposePrivateMediaStorageNamespaces(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	paths := []string{
		"v1/user/recording_audio/private.webm",
		".daily-speaking-multipart/upload/parts/1",
		".daily-speaking-metadata/v1/private.json",
	}
	handler := newTestServer(Config{}).Handler()
	for _, relativePath := range paths {
		absolutePath := filepath.Join(uploadsDir, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolutePath, []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/uploads/"+relativePath, nil)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "private") {
			t.Fatalf("%s leaked through legacy uploads handler: status=%d body=%q", relativePath, response.Code, response.Body.String())
		}
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
	newTestServer(Config{}).Handler().ServeHTTP(response, request)

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
	newTestServer(Config{}).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
