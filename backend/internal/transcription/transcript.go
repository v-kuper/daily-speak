package transcription

import (
	"regexp"
	"strings"
)

const maxTranscriptLength = 20000

var timestampPattern = regexp.MustCompile(`\[[0-9:.]+\s*-->\s*[0-9:.]+\]`)
var languagePattern = regexp.MustCompile(`^[a-z]{2,12}$`)

type Error struct {
	Message string
	Status  int
}

func (e Error) Error() string { return e.Message }

func normalizeTranscript(raw string) string {
	clean := timestampPattern.ReplaceAllString(raw, " ")
	return truncate(strings.Join(strings.Fields(clean), " "), maxTranscriptLength)
}

func normalizeLanguage(raw string) string {
	language := strings.ToLower(strings.TrimSpace(raw))
	if language == "auto" || !languagePattern.MatchString(language) {
		return ""
	}
	return language
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
