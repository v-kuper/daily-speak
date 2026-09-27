package shadowing

import (
	"regexp"
	"strings"
)

var unsafePathSegment = regexp.MustCompile(`[^a-z0-9_-]+`)

func sanitizePathSegment(value string) string {
	normalized := unsafePathSegment.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "_")
	runes := []rune(normalized)
	if len(runes) > 80 {
		normalized = string(runes[:80])
	}
	if normalized == "" {
		return "user"
	}
	return normalized
}
