package recording

import (
	"regexp"
)

const defaultAnalysisConcurrency = 3

var (
	cyrillicPhrasePattern = regexp.MustCompile(`[\p{Cyrillic}]+(?:[- \t]+[\p{Cyrillic}]+)*`)
	latinLetterPattern    = regexp.MustCompile(`[A-Za-z]`)
)

// Internal aliases keep the analysis implementation concise while the public
// names form the stable contract used by delivery adapters.
type suggestion = Suggestion
type suggestionCategory = SuggestionCategory
type suggestionSeverity = SuggestionSeverity

const (
	categoryLanguageSwitch    = CategoryLanguageSwitch
	categoryVerbGrammar       = CategoryVerbGrammar
	categoryNounsDeterminers  = CategoryNounsDeterminers
	categoryPrepositions      = CategoryPrepositions
	categoryVocabulary        = CategoryVocabulary
	categorySentenceStructure = CategorySentenceStructure
	categoryNaturalness       = CategoryNaturalness
	severityMajor             = SeverityMajor
	severityMedium            = SeverityMedium
	severityMinor             = SeverityMinor
)

func extractRussianPhrases(transcript string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, phrase := range cyrillicPhrasePattern.FindAllString(transcript, -1) {
		if _, exists := seen[phrase]; exists {
			continue
		}
		seen[phrase] = struct{}{}
		out = append(out, phrase)
	}
	return out
}

func containsCyrillic(value string) bool               { return cyrillicPhrasePattern.MatchString(value) }
func containsLatinLetter(value string) bool            { return latinLetterPattern.MatchString(value) }
func recordingTranscriptForPrompt(value string) string { return NormalizeTranscript(value) }
func learningReferenceFor(ruleID string, category suggestionCategory) *LearningReference {
	return ReferenceFor(ruleID, category)
}
func parseSuggestionSeverity(value string) (suggestionSeverity, bool) {
	return ParseSuggestionSeverity(value)
}
func validSuggestionCategory(value suggestionCategory) bool { return ValidSuggestionCategory(value) }
func validSuggestionSeverity(value suggestionSeverity) bool { return ValidSuggestionSeverity(value) }

func chooseString(condition bool, ifTrue, ifFalse string) string {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func chooseFloat(condition bool, ifTrue, ifFalse float64) float64 {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func absMod(value, mod int) int {
	if mod <= 0 {
		return value
	}
	out := value % mod
	if out < 0 {
		return -out
	}
	return out
}

func analysisRussianPhrases(transcript string, turns []InterviewDialogueTurn) []string {
	if len(turns) == 0 {
		return extractRussianPhrases(transcript)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, turn := range turns {
		for _, phrase := range extractRussianPhrases(turn.Answer) {
			if !seen[phrase] {
				seen[phrase] = true
				out = append(out, phrase)
			}
		}
	}
	return out
}
