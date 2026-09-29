package ollamaadapter

import (
	"context"
	"testing"

	"daily-speaking-practice/backend/internal/ai"
)

type fakeChatClient struct{ body any }

func (f *fakeChatClient) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	f.body = body
	return ai.ChatResponse{Response: `{"question":"What do you enjoy?"}`}, nil
}

func TestProviderUsesCheapStructuredModeForLiveQuestions(t *testing.T) {
	client := &fakeChatClient{}
	provider := New(client)
	provider.resolveSettings = func() ai.Settings {
		return ai.Settings{Model: "test-model", IsThinkingModel: true}
	}

	content, err := provider.Complete(context.Background(), "system", "user", 0.4)
	if err != nil {
		t.Fatal(err)
	}
	if content != `{"question":"What do you enjoy?"}` {
		t.Fatalf("content=%q", content)
	}
	body, ok := client.body.(map[string]any)
	if !ok || body["model"] != "test-model" || body["stream"] != false || body["think"] != false || body["format"] != "json" {
		t.Fatalf("body=%#v", client.body)
	}
	options, ok := body["options"].(map[string]any)
	if !ok || options["temperature"] != 0.4 {
		t.Fatalf("options=%#v", body["options"])
	}
}
