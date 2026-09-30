package ollamaadapter

import (
	"context"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/recording"
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

func (p *Provider) Complete(ctx context.Context, request recording.AnalysisCompletionRequest) (string, error) {
	settings := p.resolveSettings()
	think := ai.ThinkOption(settings.IsThinkingModel)
	if request.DisableThinking {
		think = false
	}
	body := map[string]any{
		"model":  settings.Model,
		"stream": false,
		"think":  think,
		"messages": []map[string]string{
			{"role": "system", "content": request.SystemPrompt},
			{"role": "user", "content": request.UserPrompt},
		},
		"options": map[string]any{"temperature": request.Temperature, "seed": request.Seed},
	}
	if request.StrictJSON || request.ForceJSON || !settings.IsThinkingModel {
		body["format"] = "json"
	}
	payload, err := p.client.PostChat(ctx, body)
	if err != nil {
		return "", err
	}
	return ai.ExtractMessageContent(payload), nil
}
