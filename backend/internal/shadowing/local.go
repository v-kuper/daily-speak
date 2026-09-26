package shadowing

import (
	"errors"
	"os"
	"path/filepath"

	"daily-speaking-practice/backend/internal/domain"
)

type SavedFile struct {
	PublicURL    string
	AbsolutePath string
}

type LocalSaver struct{ root string }

func NewLocalSaver(root string) *LocalSaver { return &LocalSaver{root: root} }

func (s *LocalSaver) Save(userID, recordingID, attemptID string, audio []byte) (SavedFile, error) {
	if len(audio) == 0 || len(audio) > MaxAudioBytes {
		return SavedFile{}, errors.New("shadowing audio payload is invalid")
	}
	userSegment := domain.SanitizePathSegment(userID)
	recordingSegment := domain.SanitizePathSegment(recordingID)
	attemptSegment := domain.SanitizePathSegment(attemptID)
	directory := filepath.Join(s.root, "shadowing", userSegment)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return SavedFile{}, err
	}
	temporary, err := os.CreateTemp(directory, ".shadowing-*.tmp")
	if err != nil {
		return SavedFile{}, err
	}
	temporaryPath := temporary.Name()
	cleanupTemporary := true
	defer func() {
		_ = temporary.Close()
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(audio); err != nil {
		return SavedFile{}, err
	}
	if err := temporary.Sync(); err != nil {
		return SavedFile{}, err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return SavedFile{}, err
	}
	if err := temporary.Close(); err != nil {
		return SavedFile{}, err
	}
	fileName := recordingSegment + "-" + attemptSegment + ".mp3"
	absolutePath := filepath.Join(directory, fileName)
	if err := os.Remove(absolutePath); err != nil && !os.IsNotExist(err) {
		return SavedFile{}, err
	}
	if err := os.Rename(temporaryPath, absolutePath); err != nil {
		return SavedFile{}, err
	}
	cleanupTemporary = false
	return SavedFile{PublicURL: "/uploads/shadowing/" + userSegment + "/" + fileName, AbsolutePath: absolutePath}, nil
}
