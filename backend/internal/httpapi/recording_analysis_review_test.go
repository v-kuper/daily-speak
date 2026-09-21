package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReviewerCannotInventCandidateOrRemoveRussian(t *testing.T) {
	candidates := []analysisCandidate{{ID: "language_switch-001", Wrong: "капуста", Right: "cabbage", Explanation: "Use English.", Category: categoryLanguageSwitch}}
	content := `{"suggestions":[{"candidateIds":["invented-999"],"wrong":"капуста","right":"cabbage","explanation":"Use the English word here. This keeps the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null}]}`
	if _, ok := parseReviewedSuggestions(content, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("reviewer must not invent candidate IDs")
	}
	if _, ok := parseReviewedSuggestions(`{"suggestions":[]}`, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("reviewer must not remove required Russian corrections")
	}
}

func TestReviewerRejectsNullSuggestions(t *testing.T) {
	if _, ok := parseReviewedSuggestions(`{"suggestions":null}`, "I went home.", nil, nil); ok {
		t.Fatal("expected null suggestions to fail the array contract")
	}
}

func TestReviewerRejectsRussianCorrectionWithoutLatinText(t *testing.T) {
	candidates := []analysisCandidate{{ID: "language_switch-001", Wrong: "капуста", Right: "...", Explanation: "Use English.", Category: categoryLanguageSwitch}}
	content := `{"suggestions":[{"candidateIds":["language_switch-001"],"wrong":"капуста","right":"...","explanation":"Replace the Russian word with English. Keep the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null}]}`
	if _, ok := parseReviewedSuggestions(content, "I bought капуста.", candidates, []string{"капуста"}); ok {
		t.Fatal("expected Russian correction without Latin text to fail")
	}
}

func TestReviewerRejectsStyleAsMinorAndUnsupportedEnums(t *testing.T) {
	candidate := analysisCandidate{ID: "naturalness-001", Wrong: "I enjoyed the film", Right: "I liked the movie", Explanation: "Optional wording.", Category: categoryNaturalness}
	content := `{"suggestions":[{"candidateIds":["naturalness-001"],"wrong":"I enjoyed the film","right":"I liked the movie","explanation":"This is only a stylistic alternative. Both versions are natural.","category":"naturalness","severity":"tiny","ruleId":null}]}`
	if _, ok := parseReviewedSuggestions(content, "I enjoyed the film.", []analysisCandidate{candidate}, nil); ok {
		t.Fatal("unsupported severity must invalidate the reviewer response")
	}
}

func TestReviewerPromptLimitsTheModelToCandidateAdjudication(t *testing.T) {
	prompt := recordingReviewerPrompt("I am forgot капуста.", []analysisCandidate{{ID: "verb_grammar-001", Wrong: "am forgot", Right: "forgot", Explanation: "Use past simple.", Category: categoryVerbGrammar}}, []string{"капуста"})
	for _, fragment := range []string{
		"adjudicator, not an error detector",
		"must not add a new error",
		"reject acceptable conversational English",
		"minor is a real localized error, never a preference",
		`"candidateIds"`,
		`"requiredRussianPhrases":["капуста"]`,
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
	content := `{"suggestions":[
		{"candidateIds":["verb_grammar-001"],"wrong":"I am forgot","right":"I forgot","explanation":"Use the past-simple verb without am. The auxiliary cannot be combined with forgot here.","category":"verb_grammar","severity":"medium","ruleId":"verb-forms"},
		{"candidateIds":["verb_grammar-003"],"wrong":"I am forgot","right":"I forgot","explanation":"Use the past-simple verb without am. The auxiliary cannot be combined with forgot here.","category":"verb_grammar","severity":"medium","ruleId":"verb-forms"},
		{"candidateIds":["verb_grammar-002"],"wrong":"am forgot","right":"forgot","explanation":"Remove the present auxiliary am. Past simple uses forgot by itself.","category":"verb_grammar","severity":"minor","ruleId":"verb-forms"}
	]}`

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
	content := `{"suggestions":[
		{"candidateIds":["vocabulary-001"],"wrong":"green капуста","right":"green cabbage","explanation":"The phrase mixes two languages. Use one English noun phrase instead.","category":"vocabulary","severity":"medium","ruleId":null},
		{"candidateIds":["language_switch-001"],"wrong":"капуста","right":"cabbage","explanation":"Replace the Russian noun with its English equivalent. This keeps the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null}
	]}`

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
	content := `{"suggestions":[
		{"candidateIds":["language_switch-001"],"wrong":"борщ","right":"borscht","explanation":"Replace this Russian noun with its English equivalent. This keeps the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null},
		{"candidateIds":["language_switch-002"],"wrong":"красный борщ","right":"red borscht","explanation":"Replace this Russian phrase with its English equivalent. This keeps the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null},
		{"candidateIds":["language_switch-003"],"wrong":"Борщ","right":"Borscht","explanation":"Replace this capitalized Russian noun with its English equivalent. This keeps the sentence in the target language.","category":"language_switch","severity":"medium","ruleId":null}
	]}`
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
	wire := make([]map[string]any, 25)
	for index := range candidates {
		wrong := fmt.Sprintf("error-%02d", index)
		parts[index] = wrong
		id := fmt.Sprintf("verb_grammar-%03d", index+1)
		candidates[index] = analysisCandidate{ID: id, Wrong: wrong, Right: fmt.Sprintf("fixed-%02d", index), Explanation: "Detector explanation.", Category: categoryVerbGrammar}
		wire[index] = map[string]any{
			"candidateIds": []string{id},
			"wrong":        wrong,
			"right":        fmt.Sprintf("fixed-%02d", index),
			"explanation":  "This form is incorrect here. Use the corrected form in this sentence.",
			"category":     categoryVerbGrammar,
			"severity":     severityMinor,
			"ruleId":       "verb-forms",
		}
	}
	payload, err := json.Marshal(map[string]any{"suggestions": wire})
	if err != nil {
		t.Fatal(err)
	}

	got, ok := parseReviewedSuggestions(string(payload), strings.Join(parts, " "), candidates, nil)
	if !ok || len(got) != 25 || got[0].Wrong != "error-00" || got[24].Wrong != "error-24" {
		t.Fatalf("expected 25 ordered suggestions, got %#v, valid=%v", got, ok)
	}
}
