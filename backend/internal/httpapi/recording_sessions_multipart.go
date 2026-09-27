package httpapi

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/storage"
)

type recordingChunkUpload struct {
	Index     int
	Extension string
	Bytes     []byte
}

type recordingFinalAudioUpload struct {
	Extension string
	Bytes     []byte
}

func readMultipartChunkRequest(r *http.Request) (recordingChunkUpload, error) {
	if err := r.ParseMultipartForm(storage.MaxRecordingChunkBytes + 1024*1024); err != nil {
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
	data, err := io.ReadAll(io.LimitReader(file, storage.MaxRecordingChunkBytes+1))
	if err != nil {
		return recordingChunkUpload{}, err
	}
	if len(data) == 0 || len(data) > storage.MaxRecordingChunkBytes {
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
