package learner

import "strings"

const DefaultEnglishLevel = "b1"

var englishLevelSet = map[string]struct{}{
	"a1": {}, "a2": {}, "b1": {}, "b2": {}, "c1": {}, "c2": {},
}

func ParseEnglishLevel(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	_, ok := englishLevelSet[normalized]
	return normalized, ok
}

func NormalizeEnglishLevel(value string) string {
	if level, ok := ParseEnglishLevel(value); ok {
		return level
	}
	return DefaultEnglishLevel
}

func FormatEnglishLevel(level string) string {
	return strings.ToUpper(NormalizeEnglishLevel(level))
}

func EnglishLevelPromptGuidance(level string) string {
	switch NormalizeEnglishLevel(level) {
	case "a1":
		return "Use very basic vocabulary, short present-tense phrasing, and one clear idea per question."
	case "a2":
		return "Use simple everyday vocabulary, short sentences, and basic past/future forms."
	case "b2":
		return "Use richer vocabulary, nuanced scenarios, and natural connector words."
	case "c1":
		return "Use advanced vocabulary, abstract angles, and complex but natural phrasing."
	case "c2":
		return "Use near-native sophistication, idiomatic phrasing, and subtle distinctions."
	default:
		return "Use practical intermediate vocabulary and clear sentence structures."
	}
}

func NormalizeInterests(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range values {
		value := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
		if value == "" || len(value) > 80 {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
