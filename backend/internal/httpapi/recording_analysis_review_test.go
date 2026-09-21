package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReviewerBuildsSuggestionsFromFlatDecisions(t *testing.T) {
	transcript := "I am forgot капуста."
	candidates := []analysisCandidate{
		{ID: "language_switch-001", Wrong: "капуста", Right: "cabbage", Explanation: "Use the English word.", Category: categoryLanguageSwitch},
		{ID: "verb_grammar-001", Wrong: "am forgot", Right: "forgot", Explanation: "Use past simple without am.", Category: categoryVerbGrammar, RuleID: "verb-forms"},
		{ID: "naturalness-001", Wrong: "I am forgot", Right: "I forgot", Explanation: "This phrasing is not natural.", Category: categoryNaturalness},
	}
	content := `{"decisions":{"language_switch-001":"medium","verb_grammar-001":"major","naturalness-001":"reject"}}`

	got, ok := parseReviewedSuggestions(content, transcript, candidates, []string{"капуста"})
	if !ok || len(got) != 2 {
		t.Fatalf("expected two server-built suggestions, got %#v, valid=%v", got, ok)
	}
	if got[0].Wrong != "am forgot" || got[0].Explanation != "Use past simple without am." || got[0].Severity != severityMajor || got[0].RuleID != "verb-forms" {
		t.Fatalf("grammar suggestion=%#v", got[0])
	}
	if got[1].Wrong != "капуста" || got[1].Right != "cabbage" || got[1].Severity != severityMedium {
		t.Fatalf("language suggestion=%#v", got[1])
	}
}

func TestReviewerFlatDecisionsRequireEveryKnownCandidate(t *testing.T) {
	candidates := []analysisCandidate{
		{ID: "verb_grammar-001", Wrong: "am forgot", Right: "forgot", Explanation: "Use past simple.", Category: categoryVerbGrammar},
		{ID: "naturalness-001", Wrong: "I am forgot", Right: "I forgot", Explanation: "This phrasing is not natural.", Category: categoryNaturalness},
	}
	for name, content := range map[string]string{
		"missing":         `{"decisions":{"verb_grammar-001":"medium"}}`,
		"unknown":         `{"decisions":{"verb_grammar-001":"medium","invented-999":"minor"}}`,
		"invalid verdict": `{"decisions":{"verb_grammar-001":"medium","naturalness-001":"tiny"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseReviewedSuggestions(content, "I am forgot.", candidates, nil); ok {
				t.Fatal("expected incomplete or invalid decision map to fail")
			}
		})
	}
}

func TestReviewerPromptRequestsOnlyFlatDecisionValues(t *testing.T) {
	prompt := recordingReviewerPrompt("I am forgot.", []analysisCandidate{{ID: "verb_grammar-001", Wrong: "am forgot", Right: "forgot", Explanation: "Use past simple.", Category: categoryVerbGrammar}}, nil)
	if !strings.Contains(prompt, `{"decisions":{"verb_grammar-001":"medium"}}`) {
		t.Fatalf("reviewer prompt does not request the flat decision map: %s", prompt)
	}
	for _, repeatedField := range []string{`"candidateIds"`, `"severity"`, `"suggestions"`} {
		if strings.Contains(prompt, repeatedField) {
			t.Fatalf("reviewer prompt still asks the model to repeat %s", repeatedField)
		}
	}
}

func TestReviewerCannotInventCandidateOrRemoveRussian(t *testing.T) {
	candidates := []analysisCandidate{{ID: "language_switch-001", Wrong: "капуста", Right: "cabbage", Explanation: "Use English.", Category: categoryLanguageSwitch}}
	content := `{"decisions":{"invented-999":"medium"}}`
	if _, ok := parseReviewedSuggestions(content, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("reviewer must not invent candidate IDs")
	}
	if _, ok := parseReviewedSuggestions(`{"decisions":{"language_switch-001":"reject"}}`, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("reviewer must not remove required Russian corrections")
	}
}

func TestReviewerRejectsNullDecisions(t *testing.T) {
	if _, ok := parseReviewedSuggestions(`{"decisions":null}`, "I went home.", nil, nil); ok {
		t.Fatal("expected null decisions to fail the object contract")
	}
}

func TestReviewerRejectsRussianCorrectionWithoutLatinText(t *testing.T) {
	candidates := []analysisCandidate{{ID: "language_switch-001", Wrong: "капуста", Right: "...", Explanation: "Use English.", Category: categoryLanguageSwitch}}
	content := `{"decisions":{"language_switch-001":"medium"}}`
	if _, ok := parseReviewedSuggestions(content, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("expected Russian correction without Latin text to fail")
	}
}

func TestReviewerRejectsStyleAsMinorAndUnsupportedEnums(t *testing.T) {
	candidate := analysisCandidate{ID: "naturalness-001", Wrong: "I enjoyed the film", Right: "I liked the movie", Explanation: "Optional wording.", Category: categoryNaturalness}
	content := `{"decisions":{"naturalness-001":"tiny"}}`
	if _, ok := parseReviewedSuggestions(content, "I enjoyed the film.", []analysisCandidate{candidate}, nil); ok {
		t.Fatal("unsupported severity must invalidate the reviewer response")
	}
}

func TestReviewerPromptLimitsTheModelToCandidateAdjudication(t *testing.T) {
	prompt := recordingReviewerPrompt("I am forgot капуста.", []analysisCandidate{{ID: "verb_grammar-001", Wrong: "am forgot", Right: "forgot", Explanation: "Use past simple.", Category: categoryVerbGrammar}}, []string{"капуста"})
	for _, fragment := range []string{
		"adjudicator, not an error detector",
		"do not add, rewrite, merge, or omit candidates",
		"reject acceptable conversational English",
		"minor only for a real localized error, never a preference",
		"Never reject a language_switch candidate",
		`{"decisions":{"verb_grammar-001":"medium"}}`,
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("reviewer prompt missing %q", fragment)
		}
	}
}

func TestReviewedSuggestionsCollapseDuplicatesAndPreferLongerOverlap(t *testing.T) {
	transcript := "I am forgot this."
	candidates := []analysisCandidate{
		{ID: "verb_grammar-001", Wrong: "I am forgot", Right: "I forgot", Explanation: "Fix the verb form.", Category: categoryVerbGrammar},
		{ID: "verb_grammar-002", Wrong: "am forgot", Right: "forgot", Explanation: "Fix the auxiliary.", Category: categoryVerbGrammar},
		{ID: "verb_grammar-003", Wrong: "I am forgot", Right: "I forgot", Explanation: "Duplicate.", Category: categoryVerbGrammar},
	}
	content := `{"decisions":{"verb_grammar-001":"medium","verb_grammar-002":"minor","verb_grammar-003":"medium"}}`

	got, ok := parseReviewedSuggestions(content, transcript, candidates, nil)
	if !ok || len(got) != 1 || got[0].Wrong != "I am forgot" {
		t.Fatalf("expected one longer reviewed suggestion, got %#v, valid=%v", got, ok)
	}
}

func TestReviewedSuggestionsKeepMandatoryRussianOverLongerOverlap(t *testing.T) {
	transcript := "I bought green капуста."
	candidates := []analysisCandidate{
		{ID: "vocabulary-001", Wrong: "green капуста", Right: "green cabbage", Explanation: "Mixed phrase.", Category: categoryVocabulary},
		{ID: "language_switch-001", Wrong: "капуста", Right: "cabbage", Explanation: "Use English.", Category: categoryLanguageSwitch},
	}
	content := `{"decisions":{"vocabulary-001":"medium","language_switch-001":"medium"}}`

	got, ok := parseReviewedSuggestions(content, transcript, candidates, []string{"капуста"})
	if !ok || len(got) != 1 || got[0].Wrong != "капуста" {
		t.Fatalf("expected mandatory Russian correction to win, got %#v, valid=%v", got, ok)
	}
}

func TestReviewedSuggestionsPreserveDistinctNestedAndCaseVariantMandatoryRussian(t *testing.T) {
	transcript := "I ate борщ. Then I cooked красный борщ. Борщ was delicious."
	candidates := []analysisCandidate{
		{ID: "language_switch-001", Wrong: "борщ", Right: "borscht", Explanation: "Use English.", Category: categoryLanguageSwitch},
		{ID: "language_switch-002", Wrong: "красный борщ", Right: "red borscht", Explanation: "Use English.", Category: categoryLanguageSwitch},
		{ID: "language_switch-003", Wrong: "Борщ", Right: "Borscht", Explanation: "Use English.", Category: categoryLanguageSwitch},
	}
	content := `{"decisions":{"language_switch-001":"medium","language_switch-002":"medium","language_switch-003":"medium"}}`
	required := []string{"борщ", "красный борщ", "Борщ"}

	got, ok := parseReviewedSuggestions(content, transcript, candidates, required)
	if !ok || len(got) != len(required) {
		t.Fatalf("expected all distinct mandatory phrases, got %#v, valid=%v", got, ok)
	}
	for _, phrase := range required {
		matches := 0
		for _, item := range got {
			if item.Wrong == phrase {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("mandatory phrase %q appears %d times in %#v", phrase, matches, got)
		}
	}
}

func TestReviewedSuggestionsKeepAllTwentyFiveAndSortByTranscript(t *testing.T) {
	parts := make([]string, 25)
	candidates := make([]analysisCandidate, 25)
	decisions := make(map[string]string, 25)
	for index := range candidates {
		wrong := fmt.Sprintf("error-%02d", index)
		parts[index] = wrong
		id := fmt.Sprintf("verb_grammar-%03d", index+1)
		candidates[index] = analysisCandidate{ID: id, Wrong: wrong, Right: fmt.Sprintf("fixed-%02d", index), Explanation: "Detector explanation.", Category: categoryVerbGrammar}
		decisions[id] = string(severityMinor)
	}
	payload, err := json.Marshal(map[string]any{"decisions": decisions})
	if err != nil {
		t.Fatal(err)
	}

	got, ok := parseReviewedSuggestions(string(payload), strings.Join(parts, " "), candidates, nil)
	if !ok || len(got) != 25 || got[0].Wrong != "error-00" || got[24].Wrong != "error-24" {
		t.Fatalf("expected 25 ordered suggestions, got %#v, valid=%v", got, ok)
	}
}
