package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/domain"
)

type suggestion struct {
	Wrong       string `json:"wrong"`
	Right       string `json:"right"`
	Explanation string `json:"explanation"`
}

type recordingResponse struct {
	ID              string       `json:"id"`
	Topic           string       `json:"topic"`
	Duration        int          `json:"duration"`
	Timestamp       string       `json:"timestamp"`
	Status          string       `json:"status"`
	Transcript      string       `json:"transcript"`
	Suggestions     []suggestion `json:"suggestions"`
	PracticeType    string       `json:"practiceType"`
	AudioDataURL    *string      `json:"audioDataUrl"`
	PhotoDataURL    *string      `json:"photoDataUrl"`
	PhotoObject     *string      `json:"photoObject"`
	ProcessingError *string      `json:"processingError"`
}

func (s *Server) optionalUser(r *http.Request) (*auth.User, error) {
	token := sessionToken(r)
	if token == "" {
		return nil, nil
	}
	return auth.GetUserBySessionToken(r.Context(), s.db, token)
}

func decodeJSON(r *http.Request, dest any) bool {
	if r.Body == nil {
		return false
	}
	return json.NewDecoder(r.Body).Decode(dest) == nil
}

func parseIntAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func stringAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func normalizeSuggestions(input []byte, limit int) []suggestion {
	if len(input) == 0 {
		return []suggestion{}
	}
	var raw []map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return []suggestion{}
	}
	out := []suggestion{}
	for _, item := range raw {
		wrong := strings.TrimSpace(stringAny(firstValue(item, "wrong", "original", "mistake", "incorrect")))
		right := strings.TrimSpace(stringAny(firstValue(item, "right", "correct", "correction", "fixed")))
		explanation := strings.TrimSpace(stringAny(firstValue(item, "explanation", "reason", "note", "comment")))
		if wrong == "" || right == "" || explanation == "" {
			continue
		}
		out = append(out, suggestion{Wrong: wrong, Right: right, Explanation: explanation})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func firstValue(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			return value
		}
	}
	return nil
}

func normalizeURLInterests(values url.Values) []string {
	return domain.NormalizeInterests(domain.URLQueryAll(values, "interest"), 10)
}

func errorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}
