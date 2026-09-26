package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultOllamaBaseURL         = "http://127.0.0.1:11434"
	DefaultOllamaModel           = "gemma4:31b-cloud"
	defaultOllamaIsThinkingModel = true
)

type Settings struct {
	Model           string
	IsThinkingModel bool
}

type ChatResponse struct {
	Message *struct {
		Content string `json:"content"`
	} `json:"message"`
	Response string `json:"response"`
}

type ChatError struct {
	StatusCode int
	Message    string
}

func (e ChatError) Error() string {
	return e.Message
}

func DefaultModel() string {
	if model := normalizeModel(os.Getenv("OLLAMA_MODEL")); model != "" {
		return model
	}
	return DefaultOllamaModel
}

func DefaultIsThinkingModel() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("OLLAMA_THINKING_MODEL")))
	if raw == "" {
		return defaultOllamaIsThinkingModel
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func ResolveSettingsForUser() Settings {
	return Settings{Model: DefaultModel(), IsThinkingModel: DefaultIsThinkingModel()}
}

func ThinkOption(isThinkingModel bool) bool {
	return isThinkingModel
}

func BaseURL() string {
	if raw := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")); raw != "" {
		return raw
	}
	return DefaultOllamaBaseURL
}

func ExtractMessageContent(payload ChatResponse) string {
	if payload.Message != nil && strings.TrimSpace(payload.Message.Content) != "" {
		return strings.TrimSpace(payload.Message.Content)
	}
	return strings.TrimSpace(payload.Response)
}

func PostChat(ctx context.Context, body any) (ChatResponse, *http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(BaseURL(), "/")+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 120 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return ChatResponse{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ChatResponse{}, response, ChatError{
			StatusCode: response.StatusCode,
			Message:    "Ollama request failed (" + response.Status + "): " + truncate(strings.TrimSpace(string(text)), 300),
		}
	}
	var decoded ChatResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return ChatResponse{}, response, err
	}
	return decoded, response, nil
}

func IsChatError(err error) bool {
	var chatErr ChatError
	return errors.As(err, &chatErr)
}

func normalizeModel(value string) string {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" || len(cleaned) > 120 {
		return ""
	}
	return cleaned
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
