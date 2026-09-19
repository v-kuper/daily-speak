package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
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

func storedUploadPath(publicURL string) (string, error) {
	normalized := strings.TrimSpace(publicURL)
	if !strings.HasPrefix(normalized, uploadsURLPrefix) {
		return "", errors.New("stored upload URL is invalid")
	}
	segments := strings.Split(strings.TrimPrefix(normalized, uploadsURLPrefix), "/")
	if len(segments) != 3 || (segments[0] != "recordings" && segments[0] != "feed-replies") {
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
