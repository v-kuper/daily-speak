package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/domain"
)

type suggestionCategory string

type suggestionSeverity string

const (
	categoryLanguageSwitch    suggestionCategory = "language_switch"
	categoryVerbGrammar       suggestionCategory = "verb_grammar"
	categoryNounsDeterminers  suggestionCategory = "nouns_determiners"
	categoryPrepositions      suggestionCategory = "prepositions"
	categoryVocabulary        suggestionCategory = "vocabulary"
	categorySentenceStructure suggestionCategory = "sentence_structure"
	categoryNaturalness       suggestionCategory = "naturalness"

	severityMajor  suggestionSeverity = "major"
	severityMedium suggestionSeverity = "medium"
	severityMinor  suggestionSeverity = "minor"
)

type learningReference struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	URL     string `json:"url,omitempty"`
}

type suggestion struct {
	Wrong             string             `json:"wrong"`
	Right             string             `json:"right"`
	Explanation       string             `json:"explanation"`
	Category          suggestionCategory `json:"category,omitempty"`
	Severity          suggestionSeverity `json:"severity,omitempty"`
	RuleID            string             `json:"ruleId,omitempty"`
	LearningReference *learningReference `json:"learningReference,omitempty"`
}

type recordingResponse struct {
	ID                  string       `json:"id"`
	Topic               string       `json:"topic"`
	Duration            int          `json:"duration"`
	Timestamp           string       `json:"timestamp"`
	Status              string       `json:"status"`
	Transcript          string       `json:"transcript"`
	CorrectedTranscript string       `json:"correctedTranscript"`
	Suggestions         []suggestion `json:"suggestions"`
	ProcessingStage     *string      `json:"processingStage"`
	PracticeType        string       `json:"practiceType"`
	AudioDataURL        *string      `json:"audioDataUrl"`
	PhotoDataURL        *string      `json:"photoDataUrl"`
	PhotoObject         *string      `json:"photoObject"`
	ProcessingError     *string      `json:"processingError"`
	ShadowingStatus     string       `json:"shadowingStatus"`
	ShadowingAudioURL   *string      `json:"shadowingAudioUrl"`
	ShadowingError      *string      `json:"shadowingError"`
	ShadowingUpdatedAt  string       `json:"shadowingUpdatedAt"`
}

func (s *Server) optionalUser(r *http.Request) (*auth.User, error) {
	token := sessionToken(r)
	if token == "" {
		return nil, nil
	}
	return auth.GetUserBySessionToken(r.Context(), s.db, token)
}

func decodeJSON(r *http.Request, dest any) bool {
	if r.Body == nil {
		return false
	}
	return json.NewDecoder(r.Body).Decode(dest) == nil
}

func parseIntAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func stringAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func normalizeSuggestions(input []byte, limit int) []suggestion {
	if len(input) == 0 {
		return []suggestion{}
	}
	var raw []map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return []suggestion{}
	}
	out := []suggestion{}
	for _, item := range raw {
		wrong := strings.TrimSpace(stringAny(firstValue(item, "wrong", "original", "mistake", "incorrect")))
		right := strings.TrimSpace(stringAny(firstValue(item, "right", "correct", "correction", "fixed")))
		explanation := strings.TrimSpace(stringAny(firstValue(item, "explanation", "reason", "note", "comment")))
		if wrong == "" || right == "" || explanation == "" {
			continue
		}
		category, _ := parseSuggestionCategory(stringAny(item["category"]))
		severity, _ := parseSuggestionSeverity(stringAny(item["severity"]))
		ruleID := strings.TrimSpace(stringAny(item["ruleId"]))
		reference := learningReferenceFor(ruleID, category)
		if reference == nil {
			ruleID = ""
		}
		out = append(out, suggestion{
			Wrong:             wrong,
			Right:             right,
			Explanation:       explanation,
			Category:          category,
			Severity:          severity,
			RuleID:            ruleID,
			LearningReference: reference,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func parseSuggestionCategory(value string) (suggestionCategory, bool) {
	category := suggestionCategory(strings.TrimSpace(value))
	return category, validSuggestionCategory(category)
}

func validSuggestionCategory(category suggestionCategory) bool {
	switch category {
	case categoryLanguageSwitch,
		categoryVerbGrammar,
		categoryNounsDeterminers,
		categoryPrepositions,
		categoryVocabulary,
		categorySentenceStructure,
		categoryNaturalness:
		return true
	default:
		return false
	}
}

func parseSuggestionSeverity(value string) (suggestionSeverity, bool) {
	severity := suggestionSeverity(strings.TrimSpace(value))
	return severity, validSuggestionSeverity(severity)
}

func validSuggestionSeverity(severity suggestionSeverity) bool {
	switch severity {
	case severityMajor, severityMedium, severityMinor:
		return true
	default:
		return false
	}
}

func withoutLearningReference(item suggestion) suggestion {
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

func normalizeURLInterests(values url.Values) []string {
	return domain.NormalizeInterests(domain.URLQueryAll(values, "interest"), 10)
}

func errorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}
