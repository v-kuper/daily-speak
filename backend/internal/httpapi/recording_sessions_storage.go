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

func assembleRecordingSessionChunks(sessionID string, extension string, expectedCount int, outputPath string) error {
	sessionID = domain.SanitizePathSegment(sessionID)
	extension = strings.ToLower(strings.TrimSpace(extension))
	if sessionID == "" || extension == "" || expectedCount <= 0 || strings.TrimSpace(outputPath) == "" {
		return errors.New("recording session assembly is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	tempPath := outputPath + ".part"
	out, err := os.Create(tempPath)
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
