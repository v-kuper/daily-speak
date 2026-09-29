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

// EnglishQuestionPromptGuidance gives generators concrete limits for spoken
// questions. The selected profile level is treated as a ceiling by callers;
// adaptive flows may simplify below it when the learner's recent answer shows
// that a shorter question would keep the conversation moving.
func EnglishQuestionPromptGuidance(level string) string {
	switch NormalizeEnglishLevel(level) {
	case "a1":
		return "Use at most 10 words, one simple idea, common everyday words, and simple present or basic past tense. Avoid idioms, abstract nouns, hypotheticals, and multi-part questions."
	case "a2":
		return "Use at most 14 words, one clear idea, everyday vocabulary, and simple present, past, or future forms. Avoid nested clauses, idioms, and multi-part questions."
	case "b2":
		return "Use at most 24 words, one main idea, natural wider vocabulary, and no more than one dependent clause. Avoid academic wording and stacked hypotheticals."
	case "c1":
		return "Use at most 28 words and one focused idea. Nuance and abstract topics are allowed, but keep the wording natural for speech and avoid stacked questions."
	case "c2":
		return "Use at most 30 words and one focused idea. Sophisticated or idiomatic wording is allowed when natural, but avoid dense academic phrasing and stacked questions."
	default:
		return "Use at most 18 words, one main idea, familiar vocabulary, and a clear spoken structure. You may ask for one reason or example, but avoid nested hypotheticals and multi-part questions."
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
