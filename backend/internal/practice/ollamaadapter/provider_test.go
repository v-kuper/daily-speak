package ollamaadapter

import (
	"context"
	"testing"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/practice"
)

type fakeChatClient struct {
	body any
}

func (f *fakeChatClient) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	f.body = body
	return ai.ChatResponse{Response: "generated content"}, nil
}

func TestProviderTranslatesPracticeCompletionToOllamaChat(t *testing.T) {
	client := &fakeChatClient{}
	provider := New(client)
	provider.resolveSettings = func() ai.Settings {
		return ai.Settings{Model: "test-model", IsThinkingModel: true}
	}

	result, err := provider.Complete(context.Background(), practice.CompletionRequest{
		SystemPrompt: "system", UserPrompt: "user", Temperature: 0.4, Seed: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "generated content" || result.Model != "test-model" {
		t.Fatalf("unexpected completion %#v", result)
	}
	body, ok := client.body.(map[string]any)
	if !ok || body["model"] != "test-model" || body["think"] != true {
		t.Fatalf("unexpected Ollama request %#v", client.body)
	}
	options, ok := body["options"].(map[string]any)
	if !ok || options["temperature"] != 0.4 || options["seed"] != 42 {
		t.Fatalf("unexpected Ollama options %#v", body["options"])
	}
	messages, ok := body["messages"].([]map[string]string)
	if !ok || len(messages) != 2 || messages[0]["content"] != "system" || messages[1]["content"] != "user" {
		t.Fatalf("unexpected Ollama messages %#v", body["messages"])
	}
}
