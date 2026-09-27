package feed

import (
	"net/mail"
	"strings"
	"unicode"
)

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func maskEmail(value string) string {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "hidden"
	}
	parts := strings.Split(address.Address, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "hidden"
	}
	local := []rune(parts[0])
	if len(local) == 1 {
		return string(local[0]) + "***@" + parts[1]
	}
	return string(local[0]) + "***" + string(local[len(local)-1]) + "@" + parts[1]
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '-' || char == '_' {
			out.WriteRune(char)
		}
	}
	if out.Len() == 0 {
		return "unknown"
	}
	return out.String()
}
