package practice

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type fakeCompletionProvider struct {
	responses []Completion
	err       error
	calls     int
	requests  []CompletionRequest
}

func (f *fakeCompletionProvider) Complete(_ context.Context, request CompletionRequest) (Completion, error) {
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return Completion{}, f.err
	}
	if len(f.responses) == 0 {
		return Completion{}, nil
	}
	index := f.calls - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	return f.responses[index], nil
}

func newTestService(provider CompletionProvider) *Service {
	return NewService(provider)
}

func TestDailyQuestionsUsesInjectedProviderAndKeepsTransportMetadataPrivate(t *testing.T) {
	provider := &fakeCompletionProvider{responses: []Completion{{
		Content: `{"questions":["What did you learn?","What surprised you?","What comes next?"]}`,
		Model:   "test-model",
	}}}
	service := newTestService(provider)

	result, err := service.DailyQuestions(context.Background(), DailyQuestionsInput{
		DateKey: "2026-09-27", EnglishLevel: "B1", Interests: []string{"travel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"What did you learn?", "What surprised you?", "What comes next?"}
	if !reflect.DeepEqual(result.Questions, want) {
		t.Fatalf("questions = %#v, want %#v", result.Questions, want)
	}
	if result.Meta != (GenerationMeta{Model: "test-model", Attempt: 1}) {
		t.Fatalf("meta = %#v", result.Meta)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	request := provider.requests[0]
	if request.SystemPrompt == "" || request.UserPrompt == "" || request.Seed == 0 {
		t.Fatalf("unexpected provider request %#v", request)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"questions":["What did you learn?","What surprised you?","What comes next?"]}` {
		t.Fatalf("transport contract changed: %s", encoded)
	}
}

func TestDailyQuestionsRetriesInvalidProviderOutput(t *testing.T) {
	provider := &fakeCompletionProvider{responses: []Completion{
		{Content: `{"questions":[]}`, Model: "test-model"},
		{Content: `{"questions":["First?","Second?","Third?"]}`, Model: "test-model"},
	}}
	result, err := newTestService(provider).DailyQuestions(context.Background(), DailyQuestionsInput{DateKey: "2026-09-27"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || result.Meta.Attempt != 2 {
		t.Fatalf("calls = %d, meta = %#v", provider.calls, result.Meta)
	}
}

func TestDailyQuestionsRejectsInvalidInputBeforeProviderCall(t *testing.T) {
	provider := &fakeCompletionProvider{}
	_, err := newTestService(provider).DailyQuestions(context.Background(), DailyQuestionsInput{DateKey: "27-09-2026"})
	if !errors.Is(err, ErrInvalidDateKey) {
		t.Fatalf("error = %v, want ErrInvalidDateKey", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestDailyQuestionsPreservesProviderError(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	provider := &fakeCompletionProvider{err: providerErr}
	_, err := newTestService(provider).DailyQuestions(context.Background(), DailyQuestionsInput{DateKey: "2026-09-27"})
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want wrapped provider error", err)
	}
}
