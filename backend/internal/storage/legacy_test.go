package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyUploadsConfinesPathsAndRemovesFiles(t *testing.T) {
	root := t.TempDir()
	uploads := NewLegacyUploads(root)
	path, err := uploads.Path("/uploads/shadowing/user-1/audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "shadowing", "user-1", "audio.mp3") {
		t.Fatalf("path=%q", path)
	}
	if _, err := uploads.Path("/uploads/../private.txt"); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := uploads.Remove([]string{"/uploads/shadowing/user-1/audio.mp3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
