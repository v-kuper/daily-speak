package ai

import (
	"context"
	"testing"
)

func TestOllamaClientHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (OllamaClient{}).PostChat(ctx, map[string]any{"model": "test"})
	if err == nil {
		t.Fatal("expected cancelled request to fail")
	}
}
