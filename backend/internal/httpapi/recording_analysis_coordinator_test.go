package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/logging"
)

func TestAnalysisLogMetadataContainsNoLearnerTextFields(t *testing.T) {
	got := analysisLogMeta("recording-1", "verb_grammar", "valid", 2, 1500*time.Millisecond, 4)
	want := map[string]any{
		"recordingId":    "recording-1",
		"pass":           "verb_grammar",
		"attempt":        2,
		"durationMs":     int64(1500),
		"candidateCount": 4,
		"outcome":        "valid",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("analysis metadata = %#v", got)
	}
	reviewer := reviewerLogMeta("recording-1", "valid", 1, 250*time.Millisecond, 8, 6)
	reviewerWant := map[string]any{
		"recordingId": "recording-1",
		"attempt":     1,
		"durationMs":  int64(250),
		"inputCount":  8,
		"outputCount": 6,
		"outcome":     "valid",
	}
	if !reflect.DeepEqual(reviewer, reviewerWant) {
		t.Fatalf("reviewer metadata = %#v", reviewer)
	}
	for _, key := range []string{"transcript", "prompt", "response"} {
		if _, exists := got[key]; exists {
			t.Fatalf("analysis metadata contains %q", key)
		}
		if _, exists := reviewer[key]; exists {
			t.Fatalf("reviewer metadata contains %q", key)
		}
	}
}

func TestAnalysisConcurrencyDefaultsClampsAndAcceptsRange(t *testing.T) {
	t.Setenv("AI_ANALYSIS_CONCURRENCY", "")
	if got := analysisConcurrency(); got != 3 {
		t.Fatalf("default = %d", got)
	}
	for _, invalid := range []string{"0", "8", "bad"} {
		t.Setenv("AI_ANALYSIS_CONCURRENCY", invalid)
		if got := analysisConcurrency(); got != 3 {
			t.Fatalf("%q = %d", invalid, got)
		}
	}
	t.Setenv("AI_ANALYSIS_CONCURRENCY", "2")
	if got := analysisConcurrency(); got != 2 {
		t.Fatalf("accepted = %d", got)
	}
}

type blockingAnalysisClient struct {
	active     atomic.Int32
	maximum    atomic.Int32
	requests   atomic.Int32
	entered    chan struct{}
	release    chan struct{}
	transcript string
}

func (client *blockingAnalysisClient) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	active := client.active.Add(1)
	defer client.active.Add(-1)
	client.requests.Add(1)
	for {
		maximum := client.maximum.Load()
		if active <= maximum || client.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	client.entered <- struct{}{}
	<-client.release

	category := categoryFromRequestBody(body)
	wrong := map[suggestionCategory]string{
		categoryLanguageSwitch:    "капуста",
		categoryVerbGrammar:       "verb error",
		categoryNounsDeterminers:  "noun error",
		categoryPrepositions:      "preposition error",
		categoryVocabulary:        "vocabulary error",
		categorySentenceStructure: "structure error",
		categoryNaturalness:       "naturalness error",
	}[category]
	right := strings.ReplaceAll(wrong, "error", "fix")
	if category == categoryLanguageSwitch {
		right = "cabbage"
	}
	payload, _ := json.Marshal(map[string]any{"candidates": []map[string]any{{
		"wrong": wrong, "right": right, "explanation": "Detector explanation.", "ruleId": nil,
	}}})
	return ai.ChatResponse{Response: string(payload)}, nil
}

func TestRunRecordingDetectorsBoundsConcurrencyAndOrdersByPass(t *testing.T) {
	client := &blockingAnalysisClient{
		entered: make(chan struct{}, 7),
		release: make(chan struct{}),
	}
	input := recordingAnalysisInput{
		Transcript: "капуста verb error noun error preposition error vocabulary error structure error naturalness error",
		Russian:    []string{"капуста"},
	}
	done := make(chan struct{})
	var got []analysisCandidate
	var gotErr error
	go func() {
		got, gotErr = runRecordingDetectors(context.Background(), client, input, ai.Settings{Model: "test"}, 2, "recording-1", logging.Logger{})
		close(done)
	}()

	<-client.entered
	<-client.entered
	select {
	case <-client.entered:
		t.Fatal("a third detector started before one of two workers was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(client.release)
	<-done

	if gotErr != nil {
		t.Fatal(gotErr)
	}
	if client.requests.Load() != 7 || client.maximum.Load() != 2 {
		t.Fatalf("requests=%d max=%d", client.requests.Load(), client.maximum.Load())
	}
	if len(got) != 7 {
		t.Fatalf("expected seven candidates, got %#v", got)
	}
	for index, category := range analysisPassCategories() {
		if got[index].Category != category {
			t.Fatalf("candidate %d category = %q, want %q", index, got[index].Category, category)
		}
	}
}

type scriptedAnalysisClient struct {
	mu             sync.Mutex
	byCategoryCall map[suggestionCategory]int
	reviewerCalls  int
	respond        func(suggestionCategory, int) (string, error)
	review         func() string
	fullTextSeen   bool
}

func (client *scriptedAnalysisClient) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	prompt := promptFromRequestBody(body)
	client.mu.Lock()
	defer client.mu.Unlock()
	if strings.Contains(prompt, "adjudicator, not an error detector") {
		client.reviewerCalls++
		return ai.ChatResponse{Response: client.review()}, nil
	}
	category := categoryFromPrompt(prompt)
	client.byCategoryCall[category]++
	call := client.byCategoryCall[category]
	if strings.Contains(prompt, strings.Repeat("a", 6001)+" капуста") {
		client.fullTextSeen = true
	}
	content, err := client.respond(category, call)
	return ai.ChatResponse{Response: content}, err
}

func TestGenerateRecordingSuggestionsFailsWithoutPartialReview(t *testing.T) {
	client := &scriptedAnalysisClient{
		byCategoryCall: map[suggestionCategory]int{},
		respond: func(category suggestionCategory, _ int) (string, error) {
			if category == categoryNaturalness {
				return `{broken`, nil
			}
			return `{"candidates":[]}`, nil
		},
		review: func() string { return `{"suggestions":[]}` },
	}
	server := NewServer(Config{AIClient: client})
	_, err := server.generateRecordingSuggestions(context.Background(), "recording-1", "I went home.", "Home", nil, "free_talk", nil, "b1", logging.Logger{})
	if err == nil {
		t.Fatal("expected a permanently invalid detector to fail analysis")
	}
	if client.byCategoryCall[categoryNaturalness] != 2 || client.reviewerCalls != 0 {
		t.Fatalf("naturalness calls=%d reviewer calls=%d", client.byCategoryCall[categoryNaturalness], client.reviewerCalls)
	}
}

func TestGenerateRecordingSuggestionsRetriesThenReviews(t *testing.T) {
	client := &scriptedAnalysisClient{
		byCategoryCall: map[suggestionCategory]int{},
		respond: func(category suggestionCategory, call int) (string, error) {
			if category == categoryVerbGrammar && call == 1 {
				return `{broken`, nil
			}
			return `{"candidates":[]}`, nil
		},
		review: func() string { return `{"suggestions":[]}` },
	}
	server := NewServer(Config{AIClient: client})
	got, err := server.generateRecordingSuggestions(context.Background(), "recording-1", "I went home.", "Home", nil, "free_talk", nil, "b1", logging.Logger{})
	if err != nil || len(got) != 0 {
		t.Fatalf("expected retry success, got %#v, err=%v", got, err)
	}
	if client.byCategoryCall[categoryVerbGrammar] != 2 || client.reviewerCalls != 1 {
		t.Fatalf("verb calls=%d reviewer calls=%d", client.byCategoryCall[categoryVerbGrammar], client.reviewerCalls)
	}
}

func TestGenerateRecordingSuggestionsKeepsAllCandidatesAndFullTranscript(t *testing.T) {
	englishWrong := make([]string, 25)
	detectorItems := make([]map[string]any, 25)
	reviewerItems := make([]map[string]any, 0, 26)
	for index := range englishWrong {
		wrong := fmt.Sprintf("verb-error-%02d", index)
		right := fmt.Sprintf("verb-fix-%02d", index)
		englishWrong[index] = wrong
		detectorItems[index] = map[string]any{"wrong": wrong, "right": right, "explanation": "Use the correct verb form.", "ruleId": "verb-forms"}
		reviewerItems = append(reviewerItems, map[string]any{
			"candidateIds": []string{fmt.Sprintf("verb_grammar-%03d", index+1)},
			"wrong":        wrong,
			"right":        right,
			"explanation":  "This verb form is incorrect in context. Use the corrected verb form here.",
			"category":     categoryVerbGrammar,
			"severity":     severityMinor,
			"ruleId":       "verb-forms",
		})
	}
	reviewerItems = append(reviewerItems, map[string]any{
		"candidateIds": []string{"language_switch-001"},
		"wrong":        "капуста",
		"right":        "cabbage",
		"explanation":  "Replace the Russian noun with its English equivalent. This keeps the sentence in the target language.",
		"category":     categoryLanguageSwitch,
		"severity":     severityMedium,
		"ruleId":       nil,
	})
	verbPayload, _ := json.Marshal(map[string]any{"candidates": detectorItems})
	reviewerPayload, _ := json.Marshal(map[string]any{"suggestions": reviewerItems})
	client := &scriptedAnalysisClient{
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
	server := NewServer(Config{AIClient: client})
	got, err := server.generateRecordingSuggestions(context.Background(), "recording-1", transcript, "Long talk", nil, "free_talk", nil, "b1", logging.Logger{})
	if err != nil || len(got) != 26 {
		t.Fatalf("expected 26 reviewed suggestions, got %d, err=%v", len(got), err)
	}
	if !client.fullTextSeen {
		t.Fatal("expected detector prompt to retain transcript beyond rune 6000")
	}
	foundRussian := false
	for _, item := range got {
		if item.Wrong == "капуста" && item.Right == "cabbage" {
			foundRussian = true
		}
	}
	if !foundRussian {
		t.Fatal("expected Russian correction in final suggestions")
	}
}

func promptFromRequestBody(body any) string {
	payload, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	messages, ok := payload["messages"].([]map[string]string)
	if !ok {
		return ""
	}
	for _, message := range messages {
		if message["role"] == "user" {
			return message["content"]
		}
	}
	return ""
}

func categoryFromRequestBody(body any) suggestionCategory {
	return categoryFromPrompt(promptFromRequestBody(body))
}

func categoryFromPrompt(prompt string) suggestionCategory {
	for _, category := range analysisPassCategories() {
		if strings.Contains(prompt, "Your only category is "+string(category)+".") {
			return category
		}
	}
	return ""
}
