package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const LegacyUploadsURLPrefix = "/uploads/"

type LegacyUploads struct{ root string }

func NewLegacyUploads(root string) *LegacyUploads { return &LegacyUploads{root: filepath.Clean(root)} }

func (uploads *LegacyUploads) Path(publicURL string) (string, error) {
	if uploads == nil || uploads.root == "" {
		return "", errors.New("legacy uploads are not configured")
	}
	normalized := strings.TrimSpace(publicURL)
	if !strings.HasPrefix(normalized, LegacyUploadsURLPrefix) {
		return "", errors.New("stored upload URL is invalid")
	}
	segments := strings.Split(strings.TrimPrefix(normalized, LegacyUploadsURLPrefix), "/")
	if len(segments) != 3 || (segments[0] != "recordings" && segments[0] != "feed-replies" && segments[0] != "shadowing") {
		return "", errors.New("stored upload URL is outside removable directories")
	}
	for _, segment := range segments {
		if !safeLegacySegment(segment) {
			return "", errors.New("stored upload URL contains an unsafe path segment")
		}
	}
	target := filepath.Join(uploads.root, filepath.FromSlash(strings.Join(segments, "/")))
	relative, err := filepath.Rel(uploads.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("stored upload URL escapes the uploads directory")
	}
	return target, nil
}

func (uploads *LegacyUploads) Remove(publicURLs []string) error {
	seen := map[string]struct{}{}
	errorsFound := []error{}
	for _, publicURL := range publicURLs {
		path, err := uploads.Path(publicURL)
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

func safeLegacySegment(value string) bool {
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
