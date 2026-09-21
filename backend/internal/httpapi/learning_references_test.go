package httpapi

import (
	"strings"
	"testing"
)

func TestNormalizeSuggestionsPreservesOldRowsWithoutFabricatingMetadata(t *testing.T) {
	got := normalizeSuggestions([]byte(`[{"wrong":"I go","right":"I went","explanation":"Use past tense."}]`), 0)
	if len(got) != 1 || got[0].Category != "" || got[0].Severity != "" || got[0].LearningReference != nil {
		t.Fatalf("unexpected legacy suggestion: %#v", got)
	}
}

func TestNormalizeSuggestionsEnrichesKnownCompatibleRule(t *testing.T) {
	got := normalizeSuggestions([]byte(`[{"wrong":"she go","right":"she goes","explanation":"Match subject and verb.","category":"verb_grammar","severity":"medium","ruleId":"subject-verb-agreement","learningReference":{"url":"https://evil.example"}}]`), 0)
	if len(got) != 1 || got[0].LearningReference == nil || got[0].LearningReference.ID != "subject-verb-agreement" {
		t.Fatalf("expected curated reference, got %#v", got)
	}
	if strings.Contains(got[0].LearningReference.URL, "evil.example") {
		t.Fatal("model or stored JSON must not control reference URLs")
	}
}

func TestLearningReferenceRejectsUnknownAndCategoryIncompatibleRules(t *testing.T) {
	if learningReferenceFor("unknown-rule", categoryVerbGrammar) != nil {
		t.Fatal("unknown rules must be omitted")
	}
	if learningReferenceFor("articles-a-an-the", categoryPrepositions) != nil {
		t.Fatal("category-incompatible rules must be omitted")
	}
}
