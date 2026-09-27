package recording

import "strings"

func NormalizeTranscript(value string) string {
	return truncateText(strings.Join(strings.Fields(strings.TrimSpace(value)), " "), 20000)
}

func NormalizeDurationSeconds(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func hashString(value string) int {
	hash := int32(0)
	for _, char := range value {
		hash = hash*31 + int32(char)
	}
	if hash == -2147483648 {
		return 2147483647
	}
	if hash < 0 {
		return int(-hash)
	}
	return int(hash)
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
