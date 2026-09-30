package recording

import (
	"encoding/json"
	"strings"
	"time"
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
	ID                string             `json:"id,omitempty"`
	Span              *FeedbackSpan      `json:"span,omitempty"`
	Wrong             string             `json:"wrong"`
	Right             string             `json:"right"`
	Explanation       string             `json:"explanation"`
	Category          SuggestionCategory `json:"category,omitempty"`
	Severity          SuggestionSeverity `json:"severity,omitempty"`
	RuleID            string             `json:"ruleId,omitempty"`
	LearningReference *LearningReference `json:"learningReference,omitempty"`
}

// Strength is a verified excerpt that demonstrates correct, useful English.
// Learning references are resolved by the server from the rule catalog.
type Strength struct {
	ID                string             `json:"id,omitempty"`
	Span              *FeedbackSpan      `json:"span,omitempty"`
	Excerpt           string             `json:"excerpt"`
	Explanation       string             `json:"explanation"`
	Category          SuggestionCategory `json:"category"`
	RuleID            string             `json:"ruleId"`
	LearningReference *LearningReference `json:"learningReference,omitempty"`
}

// Record is the persistence-neutral representation shared by recording use
// cases. Delivery adapters are responsible for their own response formatting.
type Record struct {
	AnalysisPipeline    string
	FocusedFeedbackJSON []byte
	ShadowingScriptJSON []byte
	ID                  string
	Topic               string
	Duration            int
	Timestamp           time.Time
	Status              string
	Transcript          string
	CorrectedTranscript string
	SuggestionsJSON     []byte
	StrengthsJSON       []byte
	StrengthsStatus     string
	ProcessingStage     *string
	PracticeType        string
	PhotoObject         *string
	ProcessingError     *string
	ShadowingStatus     string
	ShadowingError      *string
	ShadowingUpdatedAt  time.Time
	AudioAssetID        *string
	PhotoAssetID        *string
	ShadowingAssetID    *string
	InterviewTurns      []InterviewTurn
}

// InterviewTurn is an optional timeline attached to a saved interview. The
// ordinary Transcript field always contains learner speech only.
type InterviewTurn struct {
	QuestionIndex       int     `json:"questionIndex,omitempty"`
	Sequence            int     `json:"sequence"`
	Question            string  `json:"question"`
	AskedAtMS           int     `json:"askedAtMs"`
	EndedAtMS           *int    `json:"endedAtMs,omitempty"`
	AnswerText          string  `json:"answerText"`
	CorrectedAnswerText string  `json:"correctedAnswerText,omitempty"`
	AnswerSource        string  `json:"answerSource"`
	AnswerAlignment     string  `json:"answerAlignment,omitempty"`
	Skipped             bool    `json:"-"`
	TranscriptStatus    string  `json:"-"`
	FinalText           *string `json:"-"`
	Provisional         *string `json:"-"`
}

// InterviewDialogueTurn gives analysis and rewrite models the immutable
// question that prompted each learner answer. The original Transcript remains
// learner-only; the corrected interview transcript alternates these questions
// with the corrected answers for shadowing.
type InterviewDialogueTurn struct {
	Sequence int    `json:"sequence"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func (turn *InterviewTurn) ResolveAnswer() {
	if turn.FinalText != nil {
		turn.AnswerText = strings.TrimSpace(*turn.FinalText)
		if turn.AnswerText == "" {
			turn.AnswerSource = "none"
			turn.AnswerAlignment = ""
		} else {
			turn.AnswerSource = "final"
			turn.AnswerAlignment = ""
		}
	} else if turn.Provisional != nil && strings.TrimSpace(*turn.Provisional) != "" {
		turn.AnswerText = strings.TrimSpace(*turn.Provisional)
		turn.AnswerSource = "provisional"
		turn.AnswerAlignment = ""
	} else {
		turn.AnswerText = ""
		turn.AnswerSource = "none"
		turn.AnswerAlignment = ""
	}
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
			ID: strings.TrimSpace(stringValue(item["id"])), Span: normalizeFeedbackSpan(item["span"]),
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

func NormalizeStrengths(input []byte, limit int) []Strength {
	if len(input) == 0 {
		return []Strength{}
	}
	var raw []map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return []Strength{}
	}
	out := []Strength{}
	seen := map[string]struct{}{}
	for _, item := range raw {
		excerpt := strings.TrimSpace(stringValue(item["excerpt"]))
		explanation := strings.TrimSpace(stringValue(item["explanation"]))
		category, validCategory := ParseSuggestionCategory(stringValue(item["category"]))
		ruleID := strings.TrimSpace(stringValue(item["ruleId"]))
		reference := ReferenceFor(ruleID, category)
		span := normalizeFeedbackSpan(item["span"])
		key := feedbackID("strength", excerpt, span)
		if excerpt == "" || explanation == "" || !validCategory || reference == nil || len([]rune(excerpt)) > 300 || len([]rune(explanation)) > 800 {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Strength{
			ID: strings.TrimSpace(stringValue(item["id"])), Span: span,
			Excerpt: excerpt, Explanation: explanation, Category: category,
			RuleID: ruleID, LearningReference: reference,
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

func WithoutStrengthLearningReference(item Strength) Strength {
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

func normalizeFeedbackSpan(value any) *FeedbackSpan {
	data, err := json.Marshal(value)
	if err != nil || value == nil {
		return nil
	}
	var span FeedbackSpan
	if json.Unmarshal(data, &span) != nil || span.Start < 0 || span.End <= span.Start || span.TurnSequence < 0 {
		return nil
	}
	return &span
}
