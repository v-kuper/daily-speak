package shadowing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalSaverWritesAttemptScopedAudio(t *testing.T) {
	saver := NewLocalSaver(t.TempDir())
	first, err := saver.Save("user-1", "recording-1", "attempt-old", []byte("ID3-old"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saver.Save("user-1", "recording-1", "attempt-new", []byte("ID3-new"))
	if err != nil {
		t.Fatal(err)
	}
	if first.AbsolutePath == second.AbsolutePath || !strings.HasPrefix(first.PublicURL, "/uploads/shadowing/") {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	content, err := os.ReadFile(second.AbsolutePath)
	if err != nil || string(content) != "ID3-new" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if filepath.Ext(second.AbsolutePath) != ".mp3" {
		t.Fatalf("path=%q", second.AbsolutePath)
	}
}

func TestLocalSaverRejectsEmptyAudio(t *testing.T) {
	if _, err := NewLocalSaver(t.TempDir()).Save("user", "recording", "attempt", nil); err == nil {
		t.Fatal("expected validation error")
	}
}
