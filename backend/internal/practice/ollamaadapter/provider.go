package ollamaadapter

import (
	"context"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/practice"
)

type Provider struct {
	client          ai.ChatClient
	resolveSettings func() ai.Settings
}

func New(client ai.ChatClient) *Provider {
	if client == nil {
		client = ai.OllamaClient{}
	}
	return &Provider{client: client, resolveSettings: ai.ResolveSettingsForUser}
}

func (p *Provider) Complete(ctx context.Context, request practice.CompletionRequest) (practice.Completion, error) {
	settings := p.resolveSettings()
	payload, err := p.client.PostChat(ctx, map[string]any{
		"model":  settings.Model,
		"stream": false,
		"think":  ai.ThinkOption(settings.IsThinkingModel),
		"messages": []map[string]string{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": request.UserPrompt},
		},
		"options": map[string]any{
			"temperature": request.Temperature,
			"seed":        request.Seed,
		},
	})
	if err != nil {
		return practice.Completion{}, err
	}
	return practice.Completion{
		Content: ai.ExtractMessageContent(payload),
		Model:   settings.Model,
	}, nil
}
