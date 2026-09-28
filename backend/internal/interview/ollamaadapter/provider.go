package ollamaadapter

import (
	"context"

	"daily-speaking-practice/backend/internal/ai"
)

type Provider struct{ client ai.ChatClient }

func New(client ai.ChatClient) *Provider {
	if client == nil {
		client = ai.OllamaClient{}
	}
	return &Provider{client: client}
}

func (p *Provider) Complete(ctx context.Context, system, user string, temperature float64) (string, error) {
	settings := ai.ResolveSettingsForUser()
	response, err := p.client.PostChat(ctx, map[string]any{
		"model": settings.Model, "stream": false, "think": ai.ThinkOption(settings.IsThinkingModel),
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"options":  map[string]any{"temperature": temperature},
	})
	if err != nil {
		return "", err
	}
	return ai.ExtractMessageContent(response), nil
}
