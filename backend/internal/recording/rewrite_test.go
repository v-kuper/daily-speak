package recording

import (
	"context"
	"errors"
	"reflect"
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
	if err != nil || got.CorrectedTranscript != "I went to the store yesterday." || len(got.CorrectedAnswers) != 0 {
		t.Fatalf("got=%#v err=%v", got, err)
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
	if err != nil || got.CorrectedTranscript != "I bought cabbage." {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if len(provider.requests) != 2 || !provider.requests[1].StrictJSON {
		t.Fatalf("requests=%#v", provider.requests)
	}
}

func TestRewriteInterviewReturnsCorrectedAnswersAndFullDialogue(t *testing.T) {
	provider := &rewriteProvider{responses: []string{`{
		"correctedAnswers":[
			{"sequence":1,"correctedAnswerText":"I went home."},
			{"sequence":2,"correctedAnswerText":"I cooked dinner."}
		]
	}`}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	turns := []InterviewDialogueTurn{
		{Sequence: 1, Question: "Where did you go?", Answer: "I go home."},
		{Sequence: 2, Question: "What did you do next?", Answer: "I cook dinner."},
	}
	got, err := service.Rewrite(context.Background(), RewriteInput{
		Transcript: "I go home. I cook dinner.", InterviewTurns: turns, EnglishLevel: "b1",
	}, discardAnalysisLogger{})
	if err != nil {
		t.Fatal(err)
	}
	wantTranscript := "Where did you go? I went home. What did you do next? I cooked dinner."
	if got.CorrectedTranscript != wantTranscript {
		t.Fatalf("corrected transcript=%q", got.CorrectedTranscript)
	}
	wantAnswers := []CorrectedInterviewAnswer{
		{Sequence: 1, CorrectedAnswerText: "I went home."},
		{Sequence: 2, CorrectedAnswerText: "I cooked dinner."},
	}
	if len(got.CorrectedAnswers) != len(wantAnswers) {
		t.Fatalf("corrected answers=%#v", got.CorrectedAnswers)
	}
	for index := range wantAnswers {
		if got.CorrectedAnswers[index] != wantAnswers[index] {
			t.Fatalf("corrected answers=%#v", got.CorrectedAnswers)
		}
	}
	prompt := provider.requests[0].UserPrompt
	for _, fragment := range []string{"Where did you go?", "What did you do next?", "immutable context", "correctedAnswerText", "same sequence"} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("prompt missing %q: %s", fragment, prompt)
		}
	}
}

func TestRewriteInterviewPreservesSequencesAcrossSkippedTurns(t *testing.T) {
	provider := &rewriteProvider{responses: []string{`{
		"correctedAnswers":[
			{"sequence":1,"correctedAnswerText":"First corrected answer."},
			{"sequence":4,"correctedAnswerText":"Fourth corrected answer."}
		]
	}`}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	got, err := service.Rewrite(context.Background(), RewriteInput{
		Transcript: "First answer. Fourth answer.",
		InterviewTurns: []InterviewDialogueTurn{
			{Sequence: 1, Question: "First question?", Answer: "First answer."},
			{Sequence: 4, Question: "Fourth question?", Answer: "Fourth answer."},
		},
		EnglishLevel: "b1",
	}, discardAnalysisLogger{})
	if err != nil {
		t.Fatal(err)
	}
	want := RewriteResult{
		CorrectedTranscript: "First question? First corrected answer. Fourth question? Fourth corrected answer.",
		CorrectedAnswers: []CorrectedInterviewAnswer{
			{Sequence: 1, CorrectedAnswerText: "First corrected answer."},
			{Sequence: 4, CorrectedAnswerText: "Fourth corrected answer."},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestRewriteInterviewRetriesWhenAnyCorrectedAnswerIsEmpty(t *testing.T) {
	provider := &rewriteProvider{responses: []string{
		`{"correctedAnswers":[{"sequence":1,"correctedAnswerText":""},{"sequence":2,"correctedAnswerText":"Second corrected answer."}]}`,
		`{"correctedAnswers":[{"sequence":1,"correctedAnswerText":"First corrected answer."},{"sequence":2,"correctedAnswerText":"Second corrected answer."}]}`,
	}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	got, err := service.Rewrite(context.Background(), RewriteInput{
		Transcript: "First answer. Second answer.",
		InterviewTurns: []InterviewDialogueTurn{
			{Sequence: 1, Question: "First question?", Answer: "First answer."},
			{Sequence: 2, Question: "Second question?", Answer: "Second answer."},
		},
	}, discardAnalysisLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || !provider.requests[1].StrictJSON {
		t.Fatalf("requests=%#v", provider.requests)
	}
	want := "First question? First corrected answer. Second question? Second corrected answer."
	if got.CorrectedTranscript != want {
		t.Fatalf("corrected transcript=%q", got.CorrectedTranscript)
	}
}

func TestRewriteInterviewRejectsMissingOrOutOfOrderAnswers(t *testing.T) {
	provider := &rewriteProvider{responses: []string{
		`{"correctedAnswers":[{"sequence":2,"correctedAnswerText":"Second corrected answer."}]}`,
		`{"correctedAnswers":[{"sequence":2,"correctedAnswerText":"Wrong order."},{"sequence":1,"correctedAnswerText":"Wrong order."}]}`,
	}}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	_, err := service.Rewrite(context.Background(), RewriteInput{
		Transcript: "First answer. Second answer.",
		InterviewTurns: []InterviewDialogueTurn{
			{Sequence: 1, Question: "First question?", Answer: "First answer."},
			{Sequence: 2, Question: "Second question?", Answer: "Second answer."},
		},
	}, discardAnalysisLogger{})
	if !errors.Is(err, ErrRewrite) {
		t.Fatalf("err=%v", err)
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
func TestShadowingRewriteKeepsProfileLevelAndAllowsOnlySmallNextLevelStretch(t *testing.T) {
	for level, next := range map[string]string{"a1": "A2", "a2": "B1", "b1": "B2", "b2": "C1", "c1": "C2"} {
		prompt := recordingInterviewNaturalVersionPrompt(RewriteInput{EnglishLevel: level, InterviewTurns: []InterviewDialogueTurn{{Sequence: 1, Question: "What happened?", Answer: "I go to the office yesterday."}}})
		for _, fragment := range []string{"Learner level: " + strings.ToUpper(level), "occasional short, useful expression from " + next, "Do not raise the whole answer by a level", "sole reference", "Never answer the question as a different person", "Fix all remaining clear errors"} {
			if !strings.Contains(prompt, fragment) {
				t.Fatalf("missing %q for %s", fragment, level)
			}
		}
	}
	if strings.Contains(recordingNaturalVersionLevelGuidance("c2"), "from C3") {
		t.Fatal("C2 must not invent a higher CEFR level")
	}
}
