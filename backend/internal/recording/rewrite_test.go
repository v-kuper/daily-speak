package recording

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type rewriteProvider struct {
	responses []string
	err       error
	requests  []AnalysisCompletionRequest
}

func (p *rewriteProvider) Complete(_ context.Context, request AnalysisCompletionRequest) (string, error) {
	p.requests = append(p.requests, request)
	if p.err != nil {
		return "", p.err
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func TestRewriteReturnsValidatedEnglishTranscript(t *testing.T) {
	provider := &rewriteProvider{responses: []string{`{"correctedTranscript":"I went to the store yesterday."}`}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	got, err := service.Rewrite(context.Background(), RewriteInput{
		Transcript:   "Yesterday I go to the store.",
		Suggestions:  []Suggestion{{Wrong: "I go", Right: "I went"}},
		EnglishLevel: "b1",
	}, discardAnalysisLogger{})
	if err != nil || got != "I went to the store yesterday." {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if len(provider.requests) != 1 || provider.requests[0].Temperature != 0.35 {
		t.Fatalf("requests=%#v", provider.requests)
	}
}

func TestRewriteRetriesInvalidJSONAndRejectsRemainingRussian(t *testing.T) {
	provider := &rewriteProvider{responses: []string{
		`{"correctedTranscript":"I bought капуста."}`,
		`{"correctedTranscript":"I bought cabbage."}`,
	}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	got, err := service.Rewrite(context.Background(), RewriteInput{Transcript: "I bought капуста.", EnglishLevel: "b1"}, discardAnalysisLogger{})
	if err != nil || got != "I bought cabbage." {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if len(provider.requests) != 2 || !provider.requests[1].StrictJSON {
		t.Fatalf("requests=%#v", provider.requests)
	}
}

func TestRewriteReturnsStableErrorOnProviderFailure(t *testing.T) {
	service := NewAnalysisService(&rewriteProvider{err: errors.New("offline")}, AnalysisConfig{Concurrency: 1})
	_, err := service.Rewrite(context.Background(), RewriteInput{Transcript: "I went home."}, discardAnalysisLogger{})
	if !errors.Is(err, ErrRewrite) {
		t.Fatalf("err=%v", err)
	}
}

func TestNaturalVersionPromptContainsOnlyCorrectionPairsAndLevelGuidance(t *testing.T) {
	prompt := recordingNaturalVersionPrompt("She go home. капуста", []Suggestion{{
		Wrong: "She go", Right: "She goes", Explanation: "Agreement.",
		Category: CategoryVerbGrammar, Severity: SeverityMedium, RuleID: "subject-verb-agreement",
	}}, "b1")
	for _, fragment := range []string{"Learner level: B1.", "Use clear intermediate vocabulary", "preserve the speaker's meaning", `"wrong":"She go"`, `"right":"She goes"`, "English-only", "капуста"} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("missing %q in %q", fragment, prompt)
		}
	}
	for _, forbidden := range []string{"learningReference", "severity", "ruleId", "category"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt contains %q", forbidden)
		}
	}
}

func TestExtractRussianPhrasesKeepsExactUniqueText(t *testing.T) {
	got := extractRussianPhrases("I bought капуста, but я не знаю how to say it по-русски, капуста.")
	want := []string{"капуста", "я не знаю", "по-русски"}
	if len(got) != len(want) {
		t.Fatalf("got=%#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got=%#v", got)
		}
	}
}
