package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

const maxRecordingChunkBytes = 8 * 1024 * 1024

type recordingChunkUpload struct {
	Index     int
	Extension string
	Bytes     []byte
}

type recordingFinalAudioUpload struct {
	Extension string
	Bytes     []byte
}

func saveRecordingSessionChunk(sessionID string, index int, extension string, data []byte) (string, error) {
	sessionID = domain.SanitizePathSegment(sessionID)
	extension = strings.ToLower(strings.TrimSpace(extension))
	if sessionID == "" || index < 0 || extension == "" || len(data) == 0 || len(data) > maxRecordingChunkBytes {
		return "", errors.New("recording chunk is invalid")
	}
	directory := recordingSessionChunksDir(sessionID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, chunkFileName(index, extension))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func saveRecordingSessionFinalAudio(sessionID string, extension string, data []byte) (string, error) {
	sessionID = domain.SanitizePathSegment(sessionID)
	extension = strings.ToLower(strings.TrimSpace(extension))
	if sessionID == "" || extension == "" || len(data) == 0 || len(data) > domain.MaxAudioUploadBytes {
		return "", errors.New("recording final audio is invalid")
	}
	directory := recordingSessionChunksDir(sessionID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "final."+extension)
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func readMultipartChunkRequest(r *http.Request) (recordingChunkUpload, error) {
	if err := r.ParseMultipartForm(maxRecordingChunkBytes + 1024*1024); err != nil {
		return recordingChunkUpload{}, err
	}
	index, err := strconv.Atoi(strings.TrimSpace(r.FormValue("chunkIndex")))
	if err != nil || index < 0 {
		return recordingChunkUpload{}, errors.New("chunk index is invalid")
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		return recordingChunkUpload{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecordingChunkBytes+1))
	if err != nil {
		return recordingChunkUpload{}, err
	}
	if len(data) == 0 || len(data) > maxRecordingChunkBytes {
		return recordingChunkUpload{}, errors.New("chunk audio is invalid")
	}
	extension := audioExtensionFromMultipart(header.Filename, header.Header.Get("Content-Type"))
	if extension == "" {
		return recordingChunkUpload{}, errors.New("chunk audio type is invalid")
	}
	return recordingChunkUpload{Index: index, Extension: extension, Bytes: data}, nil
}

func readMultipartFinalAudioRequest(r *http.Request) (recordingFinalAudioUpload, error) {
	if err := r.ParseMultipartForm(domain.MaxAudioUploadBytes + 1024*1024); err != nil {
		return recordingFinalAudioUpload{}, err
	}
	file, header, err := r.FormFile("audio")
	if err != nil {
		return recordingFinalAudioUpload{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, domain.MaxAudioUploadBytes+1))
	if err != nil {
		return recordingFinalAudioUpload{}, err
	}
	if len(data) == 0 || len(data) > domain.MaxAudioUploadBytes {
		return recordingFinalAudioUpload{}, errors.New("final audio is invalid")
	}
	extension := audioExtensionFromMultipart(header.Filename, header.Header.Get("Content-Type"))
	if extension == "" {
		return recordingFinalAudioUpload{}, errors.New("final audio type is invalid")
	}
	return recordingFinalAudioUpload{Extension: extension, Bytes: data}, nil
}

func assembleRecordingSessionAudio(sessionID string, extension string, expectedCount int, outputPath string) error {
	finalPath := recordingSessionFinalAudioPath(sessionID, extension)
	if _, err := os.Stat(finalPath); err == nil {
		return copyFileAtomic(finalPath, outputPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return assembleRecordingSessionChunks(sessionID, extension, expectedCount, outputPath)
}

func recordingSessionFinalAudioExists(sessionID string, extension string) bool {
	if strings.TrimSpace(extension) == "" {
		return false
	}
	_, err := os.Stat(recordingSessionFinalAudioPath(sessionID, extension))
	return err == nil
}

func assembleRecordingSessionChunks(sessionID string, extension string, expectedCount int, outputPath string) error {
	sessionID = domain.SanitizePathSegment(sessionID)
	extension = strings.ToLower(strings.TrimSpace(extension))
	if sessionID == "" || extension == "" || expectedCount <= 0 || strings.TrimSpace(outputPath) == "" {
		return errors.New("recording session assembly is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	out, tempPath, err := createAtomicOutput(outputPath)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	for index := 0; index < expectedCount; index++ {
		match := filepath.Join(recordingSessionChunksDir(sessionID), chunkFileName(index, extension))
		in, err := os.Open(match)
		if err != nil {
			return errors.New("recording session is missing chunk " + strconv.Itoa(index))
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()
			return err
		}
		if err := in.Close(); err != nil {
			return err
		}
	}
	if err := out.Close(); err != nil {
		out = nil
		return err
	}
	out = nil
	if err := os.Rename(tempPath, outputPath); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func recordingSessionChunksDir(sessionID string) string {
	return filepath.Join(resolveUploadsDir(), "tmp", "recording-sessions", domain.SanitizePathSegment(sessionID))
}

func recordingSessionFinalAudioPath(sessionID string, extension string) string {
	return filepath.Join(recordingSessionChunksDir(sessionID), "final."+strings.ToLower(strings.TrimSpace(extension)))
}

func chunkFileName(index int, extension string) string {
	return leftPad6(index) + "." + strings.ToLower(strings.TrimSpace(extension))
}

func audioExtensionFromMultipart(filename string, contentType string) string {
	baseContentType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if extension := domain.ResolveAudioExtension(baseContentType); extension != "" {
		return extension
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if extension != "" && len(extension) <= 10 {
		return extension
	}
	return ""
}

func leftPad6(value int) string {
	text := strconv.Itoa(value)
	for len(text) < 6 {
		text = "0" + text
	}
	return text
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := path + ".part"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func copyFileAtomic(sourcePath string, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, tempPath, err := createAtomicOutput(outputPath)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if removeTemp {
			_ = os.Remove(tempPath)
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
	if err := os.Rename(tempPath, outputPath); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func createAtomicOutput(outputPath string) (*os.File, string, error) {
	tempPath := outputPath + ".part"
	out, err := os.Create(tempPath)
	if err != nil {
		return nil, "", err
	}
	return out, tempPath, nil
}
