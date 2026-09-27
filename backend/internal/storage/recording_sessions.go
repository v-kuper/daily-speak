package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

const MaxRecordingChunkBytes = 8 * 1024 * 1024

// RecordingSessionStore owns the temporary files used while a client uploads
// one recording. Callers decide when a session is complete; the store only
// persists, assembles, and removes its bytes.
type RecordingSessionStore interface {
	SaveChunk(sessionID string, index int, extension string, data []byte) error
	SaveFinal(sessionID string, extension string, data []byte) error
	FinalExists(sessionID string, extension string) (bool, error)
	Assemble(sessionID string, extension string, expectedChunks int, outputPath string) error
	Remove(sessionID string) error
}

type LocalRecordingSessions struct {
	root string
}

func NewLocalRecordingSessions(uploadsRoot string) *LocalRecordingSessions {
	return &LocalRecordingSessions{root: strings.TrimSpace(uploadsRoot)}
}

func (store *LocalRecordingSessions) SaveChunk(sessionID string, index int, extension string, data []byte) error {
	if index < 0 || len(data) == 0 || len(data) > MaxRecordingChunkBytes {
		return errors.New("recording chunk is invalid")
	}
	directory, err := store.sessionDirectory(sessionID)
	if err != nil {
		return errors.New("recording chunk is invalid")
	}
	extension, err = normalizeAudioExtension(extension)
	if err != nil {
		return errors.New("recording chunk is invalid")
	}
	path := filepath.Join(directory, chunkFileName(index, extension))
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("save recording chunk: %w", err)
	}
	return nil
}

func (store *LocalRecordingSessions) SaveFinal(sessionID string, extension string, data []byte) error {
	if len(data) == 0 || len(data) > domain.MaxAudioUploadBytes {
		return errors.New("recording final audio is invalid")
	}
	directory, err := store.sessionDirectory(sessionID)
	if err != nil {
		return errors.New("recording final audio is invalid")
	}
	extension, err = normalizeAudioExtension(extension)
	if err != nil {
		return errors.New("recording final audio is invalid")
	}
	path := filepath.Join(directory, "final."+extension)
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("save recording final audio: %w", err)
	}
	return nil
}

func (store *LocalRecordingSessions) FinalExists(sessionID string, extension string) (bool, error) {
	directory, err := store.sessionDirectory(sessionID)
	if err != nil {
		return false, err
	}
	extension, err = normalizeAudioExtension(extension)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(filepath.Join(directory, "final."+extension))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect recording final audio: %w", err)
	}
	return info.Mode().IsRegular(), nil
}

func (store *LocalRecordingSessions) Assemble(sessionID string, extension string, expectedChunks int, outputPath string) error {
	directory, err := store.sessionDirectory(sessionID)
	if err != nil || expectedChunks < 0 || strings.TrimSpace(outputPath) == "" {
		return errors.New("recording session assembly is invalid")
	}
	extension, err = normalizeAudioExtension(extension)
	if err != nil {
		return errors.New("recording session assembly is invalid")
	}
	finalPath := filepath.Join(directory, "final."+extension)
	if info, statErr := os.Stat(finalPath); statErr == nil && info.Mode().IsRegular() {
		return copyFileAtomic(finalPath, outputPath)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect recording final audio: %w", statErr)
	}
	if expectedChunks == 0 {
		return errors.New("recording session has no audio")
	}
	return assembleChunks(directory, extension, expectedChunks, outputPath)
}

func (store *LocalRecordingSessions) Remove(sessionID string) error {
	directory, err := store.sessionDirectory(sessionID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("remove recording session files: %w", err)
	}
	return nil
}

func (store *LocalRecordingSessions) sessionDirectory(sessionID string) (string, error) {
	if store == nil || store.root == "" || strings.TrimSpace(sessionID) == "" {
		return "", errors.New("recording session storage is not configured")
	}
	sanitizedID := domain.SanitizePathSegment(sessionID)
	return filepath.Join(store.root, "tmp", "recording-sessions", sanitizedID), nil
}

func normalizeAudioExtension(extension string) (string, error) {
	extension = strings.ToLower(strings.TrimSpace(extension))
	if extension == "" || len(extension) > 10 {
		return "", errors.New("recording audio extension is invalid")
	}
	for _, char := range extension {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return "", errors.New("recording audio extension is invalid")
		}
	}
	return extension, nil
}

func assembleChunks(directory string, extension string, expectedCount int, outputPath string) error {
	out, temporaryPath, err := createAtomicOutput(outputPath)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	for index := 0; index < expectedCount; index++ {
		path := filepath.Join(directory, chunkFileName(index, extension))
		in, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("recording session is missing chunk %d", index)
		}
		if err != nil {
			return fmt.Errorf("open recording chunk %d: %w", index, err)
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		if copyErr != nil {
			return fmt.Errorf("copy recording chunk %d: %w", index, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close recording chunk %d: %w", index, closeErr)
		}
	}
	if err := out.Close(); err != nil {
		out = nil
		return fmt.Errorf("close assembled recording: %w", err)
	}
	out = nil
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish assembled recording: %w", err)
	}
	removeTemporary = false
	return nil
}

func chunkFileName(index int, extension string) string {
	return fmt.Sprintf("%06d.%s", index, extension)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".upload-*.part")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o640); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	// Windows cannot atomically replace an existing file with os.Rename. A
	// retried chunk/final upload is allowed to replace its own prior bytes.
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func copyFileAtomic(sourcePath string, outputPath string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, temporaryPath, err := createAtomicOutput(outputPath)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		out = nil
		return err
	}
	out = nil
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func createAtomicOutput(outputPath string) (*os.File, string, error) {
	directory := filepath.Dir(outputPath)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, "", err
	}
	out, err := os.CreateTemp(directory, ".assembled-*.part")
	if err != nil {
		return nil, "", err
	}
	return out, out.Name(), nil
}

var _ RecordingSessionStore = (*LocalRecordingSessions)(nil)
