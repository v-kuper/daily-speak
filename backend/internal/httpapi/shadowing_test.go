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

	saved, err := saveShadowingAudio("user-1", "recording-1", "attempt-1", []byte("ID3"))
	if err != nil {
		t.Fatalf("save shadowing audio: %v", err)
	}
	wantDirectory := filepath.Join(uploadsDir, "shadowing", "user-1")
	if filepath.Dir(saved.absolutePath) != wantDirectory {
		t.Fatalf("absolute path = %q, want directory %q", saved.absolutePath, wantDirectory)
	}
	if saved.publicURL != "/uploads/shadowing/user-1/recording-1-attempt-1.mp3" {
		t.Fatalf("public URL = %q", saved.publicURL)
	}
	data, err := os.ReadFile(saved.absolutePath)
	if err != nil || string(data) != "ID3" {
		t.Fatalf("stored bytes = %q, err=%v", data, err)
	}
	entries, err := os.ReadDir(wantDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shadowing-") {
			t.Fatalf("temporary file remained: %q", entry.Name())
		}
	}
}

func TestSaveShadowingAudioDoesNotOverwriteAnotherAttempt(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)

	first, err := saveShadowingAudio("user-1", "recording-1", "attempt-old", []byte("ID3-old"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveShadowingAudio("user-1", "recording-1", "attempt-new", []byte("ID3-new"))
	if err != nil {
		t.Fatal(err)
	}
	if first.absolutePath == second.absolutePath || first.publicURL == second.publicURL {
		t.Fatalf("attempts share path: first=%q second=%q", first.absolutePath, second.absolutePath)
	}
	for path, want := range map[string]string{
		first.absolutePath:  "ID3-old",
		second.absolutePath: "ID3-new",
	} {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("path=%q data=%q err=%v", path, data, readErr)
		}
	}
}

func TestSaveShadowingAudioRejectsEmptyPayload(t *testing.T) {
	t.Setenv("UPLOADS_DIR", t.TempDir())
	if _, err := saveShadowingAudio("user-1", "recording-1", "attempt-1", nil); err == nil {
		t.Fatal("expected empty audio to be rejected")
	}
}
