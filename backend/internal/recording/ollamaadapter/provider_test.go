package ollamaadapter

import (
	"context"
	"testing"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/recording"
)

type fakeChatClient struct{ body any }

func (f *fakeChatClient) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	f.body = body
	return ai.ChatResponse{Response: "analysis content"}, nil
}

func TestProviderTranslatesAnalysisCompletionToOllamaChat(t *testing.T) {
	client := &fakeChatClient{}
	provider := New(client)
	provider.resolveSettings = func() ai.Settings {
		return ai.Settings{Model: "test-model", IsThinkingModel: false}
	}
	content, err := provider.Complete(context.Background(), recording.AnalysisCompletionRequest{
		SystemPrompt: "system", UserPrompt: "user", Temperature: 0.2, Seed: 42, StrictJSON: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if content != "analysis content" {
		t.Fatalf("content=%q", content)
	}
	body, ok := client.body.(map[string]any)
	if !ok || body["model"] != "test-model" || body["format"] != "json" || body["think"] != false {
		t.Fatalf("body=%#v", client.body)
	}
	options, ok := body["options"].(map[string]any)
	if !ok || options["temperature"] != 0.2 || options["seed"] != 42 {
		t.Fatalf("options=%#v", body["options"])
	}
	messages, ok := body["messages"].([]map[string]string)
	if !ok || len(messages) != 2 || messages[0]["content"] != "system" || messages[1]["content"] != "user" {
		t.Fatalf("messages=%#v", body["messages"])
	}
}
