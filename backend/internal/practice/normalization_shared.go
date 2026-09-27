package practice

import "strings"

var practiceTypeSet = map[string]struct{}{
	"free_talk": {}, "topic": {}, "photo_description": {},
}

func NormalizeType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if _, ok := practiceTypeSet[normalized]; ok {
		return normalized
	}
	return "topic"
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
