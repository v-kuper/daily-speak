package practice

import (
	"regexp"
	"strings"
)

var (
	listNumberPattern       = regexp.MustCompile(`^\d+\s*[\)\.\-:]\s*`)
	listBulletPattern       = regexp.MustCompile(`^[-*]\s*`)
	trailingWordPunctuation = regexp.MustCompile(`[.,;:!?]+$`)
	trailingListPunctuation = regexp.MustCompile(`[.;]+$`)
	extraNewlinesPattern    = regexp.MustCompile(`\n{3,}`)
	extraSpacesPattern      = regexp.MustCompile(`[ \t]+`)
	questionTokenPattern    = regexp.MustCompile(`[\pL\pN]+`)
)

var questionFillerWords = map[string]struct{}{
	"a": {}, "an": {}, "are": {}, "about": {}, "did": {}, "do": {}, "does": {},
	"for": {}, "has": {}, "have": {}, "had": {}, "in": {}, "is": {}, "it": {},
	"of": {}, "on": {}, "the": {}, "to": {}, "was": {}, "were": {}, "with": {},
	"you": {}, "your": {},
}

// Keep word order so reversing cause and effect remains a distinct question.
func questionKey(value string) string {
	words := questionTokenPattern.FindAllString(strings.ToLower(value), -1)
	meaningful := make([]string, 0, len(words))
	for _, word := range words {
		if _, filler := questionFillerWords[word]; filler {
			continue
		}
		if word == "like" {
			word = "enjoy"
		}
		meaningful = append(meaningful, word)
	}
	if len(meaningful) < 2 {
		return strings.Join(words, " ")
	}
	return strings.Join(meaningful, " ")
}

func anyQuestionOverlap(items []string, avoid []string) bool {
	keys := make(map[string]struct{}, len(avoid))
	for _, question := range avoid {
		keys[questionKey(question)] = struct{}{}
	}
	for _, question := range items {
		if _, exists := keys[questionKey(question)]; exists {
			return true
		}
	}
	return false
}

func normalizeQuestions(items []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		cleaned := listNumberPattern.ReplaceAllString(strings.TrimSpace(raw), "")
		cleaned = listBulletPattern.ReplaceAllString(cleaned, "")
		cleaned = strings.Join(strings.Fields(cleaned), " ")
		if cleaned == "" {
			continue
		}
		if !strings.HasSuffix(cleaned, "?") {
			cleaned += "?"
		}
		key := questionKey(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeWords(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		cleaned := listNumberPattern.ReplaceAllString(strings.TrimSpace(raw), "")
		cleaned = listBulletPattern.ReplaceAllString(cleaned, "")
		cleaned = trailingListPunctuation.ReplaceAllString(cleaned, "")
		cleaned = strings.Join(strings.Fields(cleaned), " ")
		if cleaned == "" {
			continue
		}
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func normalizeAvoidWords(items []string) []string {
	return uniqueLowerPreserve(normalizeWordList(items))
}

func normalizeWordList(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		word := normalizeWord(raw)
		if word == "" || len(strings.Fields(word)) > 3 || len([]rune(word)) > 36 {
			continue
		}
		key := strings.ToLower(word)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, word)
	}
	return out
}

func normalizeWord(value string) string {
	out := listNumberPattern.ReplaceAllString(strings.TrimSpace(value), "")
	out = listBulletPattern.ReplaceAllString(out, "")
	out = strings.Trim(out, "\"'`")
	out = trailingWordPunctuation.ReplaceAllString(out, "")
	return strings.Join(strings.Fields(out), " ")
}

func normalizeText(value string) string {
	out := strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	out = extraNewlinesPattern.ReplaceAllString(out, "\n\n")
	out = extraSpacesPattern.ReplaceAllString(out, " ")
	if len([]rune(out)) > 20000 {
		out = string([]rune(out)[:20000])
	}
	return out
}

func validStudyPack(words []string, text string, avoidWords []string) bool {
	return len(words) == 10 && countWords(text) >= 80 &&
		!anyLowerOverlap(words, lowerSet(avoidWords)) && countMatchedWords(text, words) >= 7
}

func countWords(text string) int {
	return len(strings.Fields(strings.TrimSpace(text)))
}

func countMatchedWords(text string, words []string) int {
	lowerText := strings.ToLower(text)
	count := 0
	for _, word := range words {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(strings.ToLower(word)) + `\b`).MatchString(lowerText) {
			count++
		}
	}
	return count
}

func nonEmptyLines(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		if cleaned := strings.TrimSpace(line); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func lowerSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		if cleaned := strings.ToLower(strings.TrimSpace(item)); cleaned != "" {
			out[cleaned] = struct{}{}
		}
	}
	return out
}

func anyLowerOverlap(items []string, avoid map[string]struct{}) bool {
	for _, item := range items {
		if _, exists := avoid[strings.ToLower(item)]; exists {
			return true
		}
	}
	return false
}

func uniqueLowerPreserve(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
