package ai

import "context"

type ChatClient interface {
	PostChat(context.Context, any) (ChatResponse, error)
}

type OllamaClient struct{}

func (OllamaClient) PostChat(ctx context.Context, body any) (ChatResponse, error) {
	payload, _, err := PostChat(ctx, body)
	return payload, err
}
