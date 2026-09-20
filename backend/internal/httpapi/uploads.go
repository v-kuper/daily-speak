package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"daily-speaking-practice/backend/internal/domain"
)

const uploadsURLPrefix = "/uploads/"

func resolveUploadsDir() string {
	value := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if value != "" {
		return value
	}
	return filepath.Join("public", "uploads")
}

func uploadsHandler() http.Handler {
	return http.StripPrefix(uploadsURLPrefix, http.FileServer(http.Dir(resolveUploadsDir())))
}

func (s *Server) handleShadowingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	publicURL := domain.NormalizeStoredShadowingAudioSource(r.URL.Path)
	if publicURL == nil || *publicURL != r.URL.Path {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	user, ok := s.authorizedUser(w, r, "uploads.shadowing.get")
	if !ok {
		return
	}

	var owned bool
	if err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM recordings
			WHERE user_id = $1 AND shadowing_audio_url = $2
		)`, user.ID, *publicURL).Scan(&owned); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load pronunciation audio."})
		return
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	absolutePath, err := storedUploadPath(*publicURL)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, absolutePath)
}

func storedUploadPath(publicURL string) (string, error) {
	normalized := strings.TrimSpace(publicURL)
	if !strings.HasPrefix(normalized, uploadsURLPrefix) {
		return "", errors.New("stored upload URL is invalid")
	}
	segments := strings.Split(strings.TrimPrefix(normalized, uploadsURLPrefix), "/")
	if len(segments) != 3 || (segments[0] != "recordings" && segments[0] != "feed-replies" && segments[0] != "shadowing") {
		return "", errors.New("stored upload URL is outside removable directories")
	}
	for _, segment := range segments {
		if !isSafeStoredUploadSegment(segment) {
			return "", errors.New("stored upload URL contains an unsafe path segment")
		}
	}

	root := filepath.Clean(resolveUploadsDir())
	target := filepath.Join(root, filepath.FromSlash(strings.Join(segments, "/")))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("stored upload URL escapes the uploads directory")
	}
	return target, nil
}

func isSafeStoredUploadSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func removeStoredUploadFiles(publicURLs []string) error {
	seen := map[string]struct{}{}
	errorsFound := []error{}
	for _, publicURL := range publicURLs {
		path, err := storedUploadPath(publicURL)
		if err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("%q: %w", publicURL, err))
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errorsFound = append(errorsFound, fmt.Errorf("%q: %w", publicURL, err))
		}
	}
	return errors.Join(errorsFound...)
}
