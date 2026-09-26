package recording

import (
	"encoding/json"
	"strings"
)

type SuggestionCategory string

type SuggestionSeverity string

const (
	CategoryLanguageSwitch    SuggestionCategory = "language_switch"
	CategoryVerbGrammar       SuggestionCategory = "verb_grammar"
	CategoryNounsDeterminers  SuggestionCategory = "nouns_determiners"
	CategoryPrepositions      SuggestionCategory = "prepositions"
	CategoryVocabulary        SuggestionCategory = "vocabulary"
	CategorySentenceStructure SuggestionCategory = "sentence_structure"
	CategoryNaturalness       SuggestionCategory = "naturalness"

	SeverityMajor  SuggestionSeverity = "major"
	SeverityMedium SuggestionSeverity = "medium"
	SeverityMinor  SuggestionSeverity = "minor"
)

type LearningReference struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	URL     string `json:"url,omitempty"`
}

// Suggestion is the recording-domain representation stored by workers and
// returned by every delivery adapter (web today, mobile in the future).
type Suggestion struct {
	Wrong             string             `json:"wrong"`
	Right             string             `json:"right"`
	Explanation       string             `json:"explanation"`
	Category          SuggestionCategory `json:"category,omitempty"`
	Severity          SuggestionSeverity `json:"severity,omitempty"`
	RuleID            string             `json:"ruleId,omitempty"`
	LearningReference *LearningReference `json:"learningReference,omitempty"`
}

func NormalizeSuggestions(input []byte, limit int) []Suggestion {
	if len(input) == 0 {
		return []Suggestion{}
	}
	var raw []map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return []Suggestion{}
	}
	out := []Suggestion{}
	for _, item := range raw {
		wrong := strings.TrimSpace(stringValue(firstValue(item, "wrong", "original", "mistake", "incorrect")))
		right := strings.TrimSpace(stringValue(firstValue(item, "right", "correct", "correction", "fixed")))
		explanation := strings.TrimSpace(stringValue(firstValue(item, "explanation", "reason", "note", "comment")))
		if wrong == "" || right == "" || explanation == "" {
			continue
		}
		category, _ := ParseSuggestionCategory(stringValue(item["category"]))
		severity, _ := ParseSuggestionSeverity(stringValue(item["severity"]))
		ruleID := strings.TrimSpace(stringValue(item["ruleId"]))
		reference := ReferenceFor(ruleID, category)
		if reference == nil {
			ruleID = ""
		}
		out = append(out, Suggestion{
			Wrong: wrong, Right: right, Explanation: explanation,
			Category: category, Severity: severity, RuleID: ruleID,
			LearningReference: reference,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func ParseSuggestionCategory(value string) (SuggestionCategory, bool) {
	category := SuggestionCategory(strings.TrimSpace(value))
	return category, ValidSuggestionCategory(category)
}

func ValidSuggestionCategory(category SuggestionCategory) bool {
	switch category {
	case CategoryLanguageSwitch, CategoryVerbGrammar, CategoryNounsDeterminers,
		CategoryPrepositions, CategoryVocabulary, CategorySentenceStructure,
		CategoryNaturalness:
		return true
	default:
		return false
	}
}

func ParseSuggestionSeverity(value string) (SuggestionSeverity, bool) {
	severity := SuggestionSeverity(strings.TrimSpace(value))
	return severity, ValidSuggestionSeverity(severity)
}

func ValidSuggestionSeverity(severity SuggestionSeverity) bool {
	switch severity {
	case SeverityMajor, SeverityMedium, SeverityMinor:
		return true
	default:
		return false
	}
}

func WithoutLearningReference(item Suggestion) Suggestion {
	item.LearningReference = nil
	return item
}

func firstValue(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			return value
		}
	}
	return nil
}

func stringValue(value any) string {
	typed, _ := value.(string)
	return typed
}
