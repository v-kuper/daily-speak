package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/ai"
)

type practiceChatClient struct {
	calls int
}

func (f *practiceChatClient) PostChat(_ context.Context, _ any) (ai.ChatResponse, error) {
	f.calls++
	return ai.ChatResponse{
		Response: `{"questions":["What did you learn?","What surprised you?","What comes next?"]}`,
	}, nil
}

func TestDailyQuestionsUsesServerAIClient(t *testing.T) {
	client := &practiceChatClient{}
	handler := newTestServer(Config{AIClient: client}).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/practice/daily-questions?date=2026-09-27&level=b1", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if body := strings.TrimSpace(response.Body.String()); body != `{"questions":["What did you learn?","What surprised you?","What comes next?"]}` {
		t.Fatalf("unexpected response %s", body)
	}
	if client.calls != 1 {
		t.Fatalf("injected client calls = %d, want 1", client.calls)
	}
}

func TestDismissDailyQuestionRequiresAccount(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/practice/daily-questions/dismiss", strings.NewReader(`{"question":"What food do you like?"}`))
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
