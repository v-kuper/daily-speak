package recording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type discardAnalysisLogger struct{}

func (discardAnalysisLogger) Info(string, map[string]any) {}
func (discardAnalysisLogger) Warn(string, map[string]any) {}

func TestAnalysisLogMetadataContainsNoLearnerText(t *testing.T) {
	got := analysisLogMeta("recording-1", "verb_grammar", "valid", 2, 1500*time.Millisecond, 4)
	want := map[string]any{"recordingId": "recording-1", "pass": "verb_grammar", "attempt": 2, "durationMs": int64(1500), "candidateCount": 4, "outcome": "valid"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata=%#v", got)
	}
	reviewer := reviewerLogMeta("recording-1", "valid", 1, 250*time.Millisecond, 8, 6)
	for _, key := range []string{"transcript", "prompt", "response"} {
		if _, exists := got[key]; exists {
			t.Fatalf("detector metadata contains %q", key)
		}
		if _, exists := reviewer[key]; exists {
			t.Fatalf("reviewer metadata contains %q", key)
		}
	}
}

func TestAnalysisConfigBoundsConcurrency(t *testing.T) {
	for _, value := range []int{0, -1, 8} {
		if got := NewAnalysisService(nil, AnalysisConfig{Concurrency: value}).concurrency; got != 3 {
			t.Fatalf("concurrency=%d", got)
		}
	}
	if got := NewAnalysisService(nil, AnalysisConfig{Concurrency: 2}).concurrency; got != 2 {
		t.Fatalf("concurrency=%d", got)
	}
}

type blockingProvider struct {
	active   atomic.Int32
	maximum  atomic.Int32
	requests atomic.Int32
	entered  chan struct{}
	release  chan struct{}
}

func (p *blockingProvider) Complete(_ context.Context, request AnalysisCompletionRequest) (string, error) {
	active := p.active.Add(1)
	defer p.active.Add(-1)
	p.requests.Add(1)
	for {
		maximum := p.maximum.Load()
		if active <= maximum || p.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	p.entered <- struct{}{}
	<-p.release
	category := categoryFromPrompt(request.UserPrompt)
	wrong := map[suggestionCategory]string{
		categoryLanguageSwitch: "капуста", categoryVerbGrammar: "verb error",
		categoryNounsDeterminers: "noun error", categoryPrepositions: "preposition error",
		categoryVocabulary: "vocabulary error", categorySentenceStructure: "structure error",
		categoryNaturalness: "naturalness error",
	}[category]
	right := strings.ReplaceAll(wrong, "error", "fix")
	if category == categoryLanguageSwitch {
		right = "cabbage"
	}
	payload, _ := json.Marshal(map[string]any{"candidates": []map[string]any{{"wrong": wrong, "right": right, "explanation": "Detector explanation.", "ruleId": nil}}})
	return string(payload), nil
}

func TestAnalysisServiceBoundsDetectorConcurrencyAndKeepsPassOrder(t *testing.T) {
	provider := &blockingProvider{entered: make(chan struct{}, 7), release: make(chan struct{})}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 2})
	input := recordingAnalysisInput{Transcript: "капуста verb error noun error preposition error vocabulary error structure error naturalness error", Russian: []string{"капуста"}}
	done := make(chan struct{})
	var got []analysisCandidate
	var gotErr error
	go func() {
		got, gotErr = service.runDetectors(context.Background(), input, "recording-1", discardAnalysisLogger{})
		close(done)
	}()
	<-provider.entered
	<-provider.entered
	select {
	case <-provider.entered:
		t.Fatal("third detector started before a worker was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(provider.release)
	<-done
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	if provider.requests.Load() != 7 || provider.maximum.Load() != 2 {
		t.Fatalf("requests=%d max=%d", provider.requests.Load(), provider.maximum.Load())
	}
	for index, category := range analysisPassCategories() {
		if got[index].Category != category {
			t.Fatalf("candidate %d=%q want %q", index, got[index].Category, category)
		}
	}
}

type scriptedProvider struct {
	mu             sync.Mutex
	byCategoryCall map[suggestionCategory]int
	reviewerCalls  int
	strengthCalls  int
	respond        func(suggestionCategory, int) (string, error)
	review         func() string
	fullTextSeen   bool
}

func (p *scriptedProvider) Complete(_ context.Context, request AnalysisCompletionRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.Contains(request.UserPrompt, "adjudicator, not an error detector") {
		p.reviewerCalls++
		return p.review(), nil
	}
	if strings.Contains(request.UserPrompt, "Identify up to three genuine strengths") {
		p.strengthCalls++
		return `{"strengths":[]}`, nil
	}
	category := categoryFromPrompt(request.UserPrompt)
	p.byCategoryCall[category]++
	if strings.Contains(request.UserPrompt, strings.Repeat("a", 6001)+" капуста") {
		p.fullTextSeen = true
	}
	return p.respond(category, p.byCategoryCall[category])
}

func TestAnalysisServiceFailsWithoutPartialReview(t *testing.T) {
	provider := &scriptedProvider{
		byCategoryCall: map[suggestionCategory]int{},
		respond: func(category suggestionCategory, _ int) (string, error) {
			if category == categoryNaturalness {
				return `{broken`, nil
			}
			return `{"candidates":[]}`, nil
		},
		review: func() string { return `{"decisions":{}}` },
	}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 3})
	_, err := service.Analyze(context.Background(), AnalysisInput{RecordingID: "recording-1", Transcript: "I went home.", EnglishLevel: "b1"}, discardAnalysisLogger{})
	if !errors.Is(err, ErrAnalysis) {
		t.Fatalf("err=%v", err)
	}
	if provider.byCategoryCall[categoryNaturalness] != 2 || provider.reviewerCalls != 0 {
		t.Fatalf("naturalness=%d reviewer=%d", provider.byCategoryCall[categoryNaturalness], provider.reviewerCalls)
	}
}

func TestAnalysisServiceRetriesThenReviews(t *testing.T) {
	provider := &scriptedProvider{
		byCategoryCall: map[suggestionCategory]int{},
		respond: func(category suggestionCategory, call int) (string, error) {
			if category == categoryVerbGrammar && call == 1 {
				return `{broken`, nil
			}
			return `{"candidates":[]}`, nil
		},
		review: func() string { return `{"decisions":{}}` },
	}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 3})
	got, err := service.Analyze(context.Background(), AnalysisInput{RecordingID: "recording-1", Transcript: "I went home.", EnglishLevel: "b1"}, discardAnalysisLogger{})
	if err != nil || len(got.Suggestions) != 0 || len(got.Strengths) != 0 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if provider.byCategoryCall[categoryVerbGrammar] != 2 || provider.reviewerCalls != 0 {
		t.Fatalf("verb=%d reviewer=%d", provider.byCategoryCall[categoryVerbGrammar], provider.reviewerCalls)
	}
}

func TestAnalysisServiceKeepsAllCandidatesAndFullTranscript(t *testing.T) {
	englishWrong := make([]string, 25)
	detectorItems := make([]map[string]any, 25)
	reviewerDecisions := make(map[string]string, 26)
	for index := range englishWrong {
		wrong := fmt.Sprintf("verb-error-%02d", index)
		englishWrong[index] = wrong
		detectorItems[index] = map[string]any{"wrong": wrong, "right": fmt.Sprintf("verb-fix-%02d", index), "explanation": "Use the correct verb form.", "ruleId": "verb-forms"}
		reviewerDecisions[fmt.Sprintf("verb_grammar-%03d", index+1)] = string(severityMinor)
	}
	reviewerDecisions["language_switch-001"] = string(severityMedium)
	verbPayload, _ := json.Marshal(map[string]any{"candidates": detectorItems})
	reviewerPayload, _ := json.Marshal(map[string]any{"decisions": reviewerDecisions})
	provider := &scriptedProvider{
		byCategoryCall: map[suggestionCategory]int{},
		respond: func(category suggestionCategory, _ int) (string, error) {
			switch category {
			case categoryLanguageSwitch:
				return `{"candidates":[{"wrong":"капуста","right":"cabbage","explanation":"Use the English noun.","ruleId":null}]}`, nil
			case categoryVerbGrammar:
				return string(verbPayload), nil
			default:
				return `{"candidates":[]}`, nil
			}
		},
		review: func() string { return string(reviewerPayload) },
	}
	transcript := strings.Repeat("a", 6001) + " капуста " + strings.Join(englishWrong, " ")
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 3})
	got, err := service.Analyze(context.Background(), AnalysisInput{RecordingID: "recording-1", Transcript: transcript, EnglishLevel: "b1"}, discardAnalysisLogger{})
	if err != nil || len(got.Suggestions) != 26 {
		t.Fatalf("count=%d err=%v", len(got.Suggestions), err)
	}
	if !provider.fullTextSeen {
		t.Fatal("full transcript was truncated")
	}
}

func categoryFromPrompt(prompt string) suggestionCategory {
	for _, category := range analysisPassCategories() {
		if strings.Contains(prompt, "Your only category is "+string(category)+".") {
			return category
		}
	}
	return ""
}
