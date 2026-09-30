package recording

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func integer(value int) *int { return &value }

func TestFeedbackSpansDistinguishContextsAndUnicode(t *testing.T) {
	text := "😀 I go every day. Yesterday I go. She is going."
	span, ok := resolveFeedbackSpan(text, "I go", nil, nil, integer(2))
	if !ok || span.Start != 29 || span.End != 33 {
		t.Fatalf("span=%#v ok=%v", span, ok)
	}
	if _, ok := resolveFeedbackSpan(text, "I go", nil, nil, nil); ok {
		t.Fatal("ambiguous phrase was guessed")
	}
	if positions := phrasePositions("She is going.", "go"); len(positions) != 0 {
		t.Fatalf("substring positions=%v", positions)
	}
}

func TestDetectorRejectsCrossAnswerAndTargetsOneInterviewTurn(t *testing.T) {
	turns := []InterviewDialogueTurn{{Sequence: 1, Answer: "I went home"}, {Sequence: 3, Answer: "I made dinner"}}
	if _, ok := parseDetectorCandidates(`{"candidates":[{"wrong":"home I made","right":"home, and I made","explanation":"Connect clauses."}]}`, categorySentenceStructure, "I went home I made dinner", turns); ok {
		t.Fatal("accepted cross-answer phrase")
	}
	turns = []InterviewDialogueTurn{{Sequence: 1, Answer: "I go home"}, {Sequence: 3, Answer: "I go home"}}
	got, ok := parseDetectorCandidates(`{"candidates":[{"wrong":"I go","right":"I went","explanation":"Past time in this answer.","turnSequence":3,"occurrence":1}]}`, categoryVerbGrammar, "I go home I go home", turns)
	if !ok || len(got) != 1 || got[0].Span.TurnSequence != 3 {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
}

func TestReviewPreservesIndependentErrorsAndResolvesPartialOverlap(t *testing.T) {
	items := []Suggestion{
		{Wrong: "go", Right: "goes", Category: CategoryVerbGrammar, Severity: SeverityMedium, Span: &FeedbackSpan{Start: 4, End: 6}},
		{Wrong: "go to home", Right: "go home", Category: CategoryPrepositions, Severity: SeverityMedium, Span: &FeedbackSpan{Start: 18, End: 28}},
	}
	if got := deduplicateReviewedSuggestions("She go home. They go to home.", items, nil); len(got) != 2 {
		t.Fatalf("got=%#v", got)
	}
	items = []Suggestion{
		{Wrong: "I very like", Right: "I really like", Category: CategorySentenceStructure, Severity: SeverityMedium, Span: &FeedbackSpan{Start: 0, End: 11}},
		{Wrong: "very like this", Right: "like this very much", Category: CategoryNaturalness, Severity: SeverityMedium, Span: &FeedbackSpan{Start: 2, End: 16}},
	}
	if got := deduplicateReviewedSuggestions("I very like this.", items, nil); len(got) != 1 || got[0].Category != CategorySentenceStructure {
		t.Fatalf("got=%#v", got)
	}
}

func TestStrengthOverlapUsesOccurrencesAndTurns(t *testing.T) {
	strengths := []Strength{{Excerpt: "I go home every day", Span: &FeedbackSpan{Start: 0, End: 19}}}
	if got := strengthsWithoutCorrectionOverlap(strengths, []Suggestion{{Wrong: "go", Span: &FeedbackSpan{Start: 24, End: 26}}}); len(got) != 1 {
		t.Fatal("lost independent strength")
	}
	if got := strengthsWithoutCorrectionOverlap(strengths, []Suggestion{{Span: &FeedbackSpan{Start: 17, End: 24}}}); len(got) != 0 {
		t.Fatal("kept partial overlap")
	}
	if got := strengthsWithoutCorrectionOverlap(strengths, []Suggestion{{Span: &FeedbackSpan{Start: 2, End: 4, TurnSequence: 2}}}); len(got) != 1 {
		t.Fatal("mixed answers")
	}
}

func TestLanguageSwitchCoversEveryExactOccurrence(t *testing.T) {
	content := `{"candidates":[{"wrong":"борщ","right":"borscht","explanation":"Use English.","occurrence":1},{"wrong":"борщ","right":"borscht","explanation":"Use English.","occurrence":2}]}`
	got, ok := parseDetectorCandidates(content, categoryLanguageSwitch, "I ate борщ. Then I made борщ.")
	if !ok || len(got) != 2 || got[0].Span.Start == got[1].Span.Start {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
	if _, ok := parseDetectorCandidates(`{"candidates":[{"wrong":"борщ","right":"borscht","explanation":"Use English.","occurrence":1}]}`, categoryLanguageSwitch, "I ate борщ. Then I made борщ."); ok {
		t.Fatal("accepted incomplete occurrence coverage")
	}
}

type memoryAnalysisCheckpoint struct {
	mu     sync.Mutex
	passes map[SuggestionCategory][]analysisCandidate
}

func (c *memoryAnalysisCheckpoint) Load(_ context.Context, category SuggestionCategory) ([]analysisCandidate, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.passes[category]
	return append([]analysisCandidate{}, data...), ok, nil
}
func (c *memoryAnalysisCheckpoint) Save(_ context.Context, category SuggestionCategory, data []analysisCandidate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.passes[category] = append([]analysisCandidate{}, data...)
	return nil
}

func TestRetryReusesSuccessfulPassesAndSkipsEmptyModelCalls(t *testing.T) {
	checkpoint := &memoryAnalysisCheckpoint{passes: map[SuggestionCategory][]analysisCandidate{}}
	provider := &scriptedProvider{byCategoryCall: map[suggestionCategory]int{}, respond: func(category suggestionCategory, call int) (string, error) {
		if category == categoryNaturalness && call <= 2 {
			return `{broken`, nil
		}
		return `{"candidates":[]}`, nil
	}, review: func() string { return `{"decisions":{}}` }}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 3})
	input := AnalysisInput{Transcript: "I went home.", Checkpoint: checkpoint}
	if _, err := service.Analyze(context.Background(), input, discardAnalysisLogger{}); err == nil {
		t.Fatal("expected failed pass")
	}
	if _, err := service.Analyze(context.Background(), input, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	if provider.byCategoryCall[categoryVerbGrammar] != 1 || provider.byCategoryCall[categoryNaturalness] != 3 || provider.byCategoryCall[categoryLanguageSwitch] != 0 || provider.reviewerCalls != 0 || provider.strengthCalls != 0 {
		t.Fatalf("provider=%#v", provider)
	}
}

func TestFeedbackPromptsTeachContextWithoutStyleFalsePositives(t *testing.T) {
	prompt := recordingDetectorPrompt(findAnalysisPass(CategoryVerbGrammar), recordingAnalysisInput{Transcript: "Yesterday I go home."})
	for _, expected := range []string{"practical rule", "intended meaning", "occurrence", "leave correct occurrences unchanged", "within one answer"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("missing %q", expected)
		}
	}
}
