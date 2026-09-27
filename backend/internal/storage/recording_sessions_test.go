package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRecordingSessionsSavesOrderedChunks(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)

	if err := store.SaveChunk("session-123", 2, "webm", []byte("chunk-two")); err != nil {
		t.Fatalf("expected chunk save to succeed: %v", err)
	}

	expected := filepath.Join(uploadsDir, "tmp", "recording-sessions", "session-123", "000002.webm")
	saved, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("expected chunk file to exist: %v", err)
	}
	if string(saved) != "chunk-two" {
		t.Fatalf("expected chunk bytes to be saved, got %q", string(saved))
	}
}

func TestLocalRecordingSessionsAssemblesChunksInOrder(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)

	if err := store.SaveChunk("session-123", 0, "webm", []byte("zero-")); err != nil {
		t.Fatalf("expected first chunk save: %v", err)
	}
	if err := store.SaveChunk("session-123", 2, "webm", []byte("two")); err != nil {
		t.Fatalf("expected third chunk save: %v", err)
	}
	if err := store.SaveChunk("session-123", 1, "webm", []byte("one-")); err != nil {
		t.Fatalf("expected second chunk save: %v", err)
	}

	outPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := store.Assemble("session-123", "webm", 3, outPath); err != nil {
		t.Fatalf("expected assembly to succeed: %v", err)
	}

	assembled, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected assembled audio file: %v", err)
	}
	if string(assembled) != "zero-one-two" {
		t.Fatalf("expected chunks concatenated in index order, got %q", string(assembled))
	}
}

func TestLocalRecordingSessionsRejectsMissingChunk(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)

	if err := store.SaveChunk("session-123", 0, "webm", []byte("zero-")); err != nil {
		t.Fatalf("expected first chunk save: %v", err)
	}
	if err := store.SaveChunk("session-123", 2, "webm", []byte("two")); err != nil {
		t.Fatalf("expected third chunk save: %v", err)
	}

	outPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := store.Assemble("session-123", "webm", 3, outPath); err == nil {
		t.Fatal("expected assembly to fail when a chunk is missing")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("expected missing chunk assembly to avoid final file, got stat error %v", err)
	}
}

func TestLocalRecordingSessionsSavesCompleteAudio(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)

	if err := store.SaveFinal("session-123", "webm", []byte{0x1a, 0x45, 0xdf, 0xa3}); err != nil {
		t.Fatalf("expected final audio save to succeed: %v", err)
	}

	expected := filepath.Join(uploadsDir, "tmp", "recording-sessions", "session-123", "final.webm")
	saved, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("expected final audio file to exist: %v", err)
	}
	if !bytes.Equal(saved, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		t.Fatalf("expected final audio bytes to be saved, got %x", saved)
	}
}

func TestLocalRecordingSessionsPrefersCompleteAudio(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)

	if err := store.SaveChunk("session-123", 0, "webm", []byte("bad chunk bytes")); err != nil {
		t.Fatalf("expected chunk save: %v", err)
	}
	if err := store.SaveFinal("session-123", "webm", []byte("complete final audio")); err != nil {
		t.Fatalf("expected final audio save: %v", err)
	}

	outPath := filepath.Join(uploadsDir, "recordings", "user-123", "recording-456.webm")
	if err := store.Assemble("session-123", "webm", 1, outPath); err != nil {
		t.Fatalf("expected assembly to prefer final audio: %v", err)
	}

	assembled, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected assembled audio file: %v", err)
	}
	if string(assembled) != "complete final audio" {
		t.Fatalf("expected final audio bytes, got %q", string(assembled))
	}
}

func TestLocalRecordingSessionsRemovesOnlyRequestedSession(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)
	if err := store.SaveFinal("session-one", "webm", []byte("one")); err != nil {
		t.Fatalf("save first session: %v", err)
	}
	if err := store.SaveFinal("session-two", "webm", []byte("two")); err != nil {
		t.Fatalf("save second session: %v", err)
	}
	secondPath := filepath.Join(uploadsDir, "tmp", "recording-sessions", "session-two", "final.webm")

	if err := store.Remove("session-one"); err != nil {
		t.Fatalf("remove first session: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("expected second session to remain: %v", err)
	}
}

func TestLocalRecordingSessionsPublishesAndDiscardsRecording(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)
	if err := store.SaveFinal("session-id", "webm", []byte("audio")); err != nil {
		t.Fatalf("save final audio: %v", err)
	}

	publicURL, err := store.Publish("session-id", "user-id", "recording-id", "webm", 0)
	if err != nil {
		t.Fatalf("publish recording: %v", err)
	}
	if publicURL != "/uploads/recordings/user-id/recording-id.webm" {
		t.Fatalf("unexpected public URL %q", publicURL)
	}
	publishedPath := filepath.Join(uploadsDir, "recordings", "user-id", "recording-id.webm")
	if saved, err := os.ReadFile(publishedPath); err != nil || string(saved) != "audio" {
		t.Fatalf("expected published audio, got %q, %v", string(saved), err)
	}

	if err := store.DiscardPublished(publicURL); err != nil {
		t.Fatalf("discard recording: %v", err)
	}
	if _, err := os.Stat(publishedPath); !os.IsNotExist(err) {
		t.Fatalf("expected published audio removal, got %v", err)
	}
}

func TestLocalRecordingSessionsAllowsIdempotentChunkReplacement(t *testing.T) {
	uploadsDir := t.TempDir()
	store := NewLocalRecordingSessions(uploadsDir)
	if err := store.SaveChunk("session-id", 0, "webm", []byte("first")); err != nil {
		t.Fatalf("save initial chunk: %v", err)
	}
	if err := store.SaveChunk("session-id", 0, "webm", []byte("replacement")); err != nil {
		t.Fatalf("replace chunk: %v", err)
	}

	path := filepath.Join(uploadsDir, "tmp", "recording-sessions", "session-id", "000000.webm")
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement chunk: %v", err)
	}
	if string(saved) != "replacement" {
		t.Fatalf("expected replacement bytes, got %q", string(saved))
	}
}
