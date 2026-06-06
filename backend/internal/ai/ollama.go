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

func NormalizeContent(value string) string {
	noThinking := stripThinkingBlocks(value)
	return strings.TrimSpace(stripMarkdownFence(noThinking))
}

func ExtractJSONCandidates(content string) []string {
	normalized := NormalizeContent(content)
	candidates := []string{}
	if normalized != "" {
		candidates = append(candidates, normalized)
	}
	for _, object := range extractJSONObjects(normalized) {
		if !contains(candidates, object) {
			candidates = append(candidates, object)
		}
	}
	return candidates
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

func stripThinkingBlocks(value string) string {
	out := value
	for {
		lower := strings.ToLower(out)
		start := strings.Index(lower, "<think>")
		end := strings.Index(lower, "</think>")
		if start < 0 || end < start {
			break
		}
		out = out[:start] + " " + out[end+len("</think>"):]
	}
	for {
		lower := strings.ToLower(out)
		start := strings.Index(lower, "<thinking>")
		end := strings.Index(lower, "</thinking>")
		if start < 0 || end < start {
			break
		}
		out = out[:start] + " " + out[end+len("</thinking>"):]
	}
	return out
}

func stripMarkdownFence(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```")
	if newline := strings.Index(trimmed, "\n"); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func extractJSONObjects(value string) []string {
	objects := []string{}
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i, r := range value {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		if r == '"' {
			inString = true
			continue
		}
		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
			continue
		}
		if r == '}' && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, value[start:i+1])
				start = -1
			}
		}
	}
	return objects
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
