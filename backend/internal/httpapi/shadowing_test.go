package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveShadowingAudioUsesAtomicStoredMP3Path(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	saved, err := saveShadowingAudio("user-1", "recording-1", []byte("ID3"))
	if err != nil {
		t.Fatalf("save shadowing audio: %v", err)
	}
	wantPath := filepath.Join(uploadsDir, "shadowing", "user-1", "recording-1.mp3")
	if saved.absolutePath != wantPath {
		t.Fatalf("absolute path = %q, want %q", saved.absolutePath, wantPath)
	}
	if saved.publicURL != "/uploads/shadowing/user-1/recording-1.mp3" {
		t.Fatalf("public URL = %q", saved.publicURL)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil || string(data) != "ID3" {
		t.Fatalf("stored bytes = %q, err=%v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(wantPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shadowing-") {
			t.Fatalf("temporary file remained: %q", entry.Name())
		}
	}
}

func TestSaveShadowingAudioRejectsEmptyPayload(t *testing.T) {
	t.Setenv("UPLOADS_DIR", t.TempDir())
	if _, err := saveShadowingAudio("user-1", "recording-1", nil); err == nil {
		t.Fatal("expected empty audio to be rejected")
	}
}
