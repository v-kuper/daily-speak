package recording

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseStrengthsRequiresVerifiedExactTextAndSupportedRule(t *testing.T) {
	content := `{"strengths":[{"excerpt":"I have lived here for five years","explanation":"You used the present perfect correctly for an unfinished time period.","category":"verb_grammar","ruleId":"past-simple-vs-present-perfect"}]}`
	got, valid := parseStrengths(content, "I have lived here for five years, and I enjoy it.")
	if !valid || len(got) != 1 || got[0].LearningReference == nil {
		t.Fatalf("strengths=%#v valid=%v", got, valid)
	}
	for _, invalid := range []string{
		`{"strengths":[{"excerpt":"I lived there","explanation":"Correct.","category":"verb_grammar","ruleId":"verb-forms"}]}`,
		`{"strengths":[{"excerpt":"I have lived here for five years","explanation":"Правильно.","category":"verb_grammar","ruleId":"verb-forms"}]}`,
		`{"strengths":[{"excerpt":"I have lived here for five years","explanation":"Correct.","category":"verb_grammar","ruleId":"unknown-rule"}]}`,
	} {
		if _, ok := parseStrengths(invalid, "I have lived here for five years, and I enjoy it."); ok {
			t.Fatalf("accepted unsupported strength: %s", invalid)
		}
	}
}

func TestParseStrengthsRejectsMoreThanThreeAndDuplicates(t *testing.T) {
	item := `{"excerpt":"I enjoy it","explanation":"Natural wording.","category":"naturalness","ruleId":"collocations"}`
	if _, valid := parseStrengths(`{"strengths":[`+strings.TrimSuffix(strings.Repeat(item+",", 4), ",")+`]}`, "I enjoy it"); valid {
		t.Fatal("accepted more than three strengths")
	}
	if _, valid := parseStrengths(`{"strengths":[`+item+`,`+item+`]}`, "I enjoy it"); valid {
		t.Fatal("accepted duplicate strengths")
	}
}

func TestParseStrengthsKeepsInterviewExcerptInsideOneLearnerAnswer(t *testing.T) {
	content := `{"strengths":[{"excerpt":"home I made","explanation":"Clear sequence.","category":"sentence_structure","ruleId":"word-order"}]}`
	turns := []InterviewDialogueTurn{{Sequence: 1, Answer: "I went home"}, {Sequence: 2, Answer: "I made dinner"}}
	if _, valid := parseStrengths(content, "I went home I made dinner", turns); valid {
		t.Fatal("accepted an excerpt assembled across separate interview answers")
	}
}

type unavailableStrengthProvider struct{ strengthCalls atomic.Int32 }

func (p *unavailableStrengthProvider) Complete(_ context.Context, request AnalysisCompletionRequest) (string, error) {
	switch {
	case strings.Contains(request.UserPrompt, "Identify up to three genuine strengths"):
		p.strengthCalls.Add(1)
		return `{broken`, nil
	case strings.Contains(request.UserPrompt, "adjudicator, not an error detector"):
		return `{"decisions":{}}`, nil
	default:
		return `{"candidates":[]}`, nil
	}
}

func TestStrengthFailureDoesNotBlockCorrections(t *testing.T) {
	provider := &unavailableStrengthProvider{}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 3})
	result, err := service.Analyze(context.Background(), AnalysisInput{
		RecordingID: "recording-1", Transcript: "I went home.", EnglishLevel: "b1",
	}, discardAnalysisLogger{})
	if err != nil || len(result.Suggestions) != 0 || len(result.Strengths) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if provider.strengthCalls.Load() != 2 {
		t.Fatalf("strength calls=%d", provider.strengthCalls.Load())
	}
}

func TestCorrectionsWinWhenStrengthsOverlap(t *testing.T) {
	strengths := []Strength{
		{Excerpt: "I goed home", Explanation: "Clear sentence.", Category: CategorySentenceStructure, RuleID: "word-order"},
		{Excerpt: "after work", Explanation: "Useful phrase.", Category: CategoryNaturalness, RuleID: "collocations"},
	}
	got := strengthsWithoutCorrectionOverlap(strengths, []Suggestion{{Wrong: "goed", Right: "went"}})
	if len(got) != 1 || got[0].Excerpt != "after work" {
		t.Fatalf("strengths=%#v", got)
	}
}
