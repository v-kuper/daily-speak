package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/practice"
)

var practiceDateKeyPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Server) handleDailyQuestions(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.daily-questions.get", r)
	values := r.URL.Query()
	dateKey := values.Get("date")
	if !practiceDateKeyPattern.MatchString(dateKey) {
		logger.Warn("request.rejected", map[string]any{
			"status": 400, "durationMs": logging.ElapsedMs(started), "reason": "invalid_date",
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query param `date` must be in YYYY-MM-DD format."})
		return
	}

	level, ok := s.practiceEnglishLevel(w, r, values.Get("level"))
	if !ok {
		return
	}
	result, err := s.practiceGenerator.DailyQuestions(r.Context(), practice.DailyQuestionsInput{
		DateKey: dateKey, RefreshToken: values.Get("refresh"), EnglishLevel: level,
		Interests: normalizeURLInterests(values), AvoidQuestions: values["avoid"],
	})
	if err != nil {
		writePracticeGenerationError(
			w, err, practice.ErrQuestionsExhausted,
			"Could not generate a sufficiently new set of questions. Try regenerate again.",
		)
		return
	}
	logger.Info("request.success", map[string]any{
		"status": 200, "durationMs": logging.ElapsedMs(started),
		"model": result.Meta.Model, "attempt": result.Meta.Attempt,
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTopicGuidance(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.topic-guidance.get", r)
	values := r.URL.Query()
	topic := strings.TrimSpace(values.Get("topic"))
	if topic == "" {
		logger.Warn("request.rejected", map[string]any{
			"status": 400, "durationMs": logging.ElapsedMs(started), "reason": "missing_topic",
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query param `topic` is required."})
		return
	}
	if len([]rune(topic)) > 300 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Topic is too long."})
		return
	}

	level, ok := s.practiceEnglishLevel(w, r, values.Get("level"))
	if !ok {
		return
	}
	result, err := s.practiceGenerator.TopicGuidance(r.Context(), practice.TopicGuidanceInput{
		Topic: topic, RefreshToken: values.Get("refresh"), EnglishLevel: level,
		Interests: normalizeURLInterests(values), AvoidQuestions: values["avoidQuestion"],
		AvoidWords: values["avoidWord"],
	})
	if err != nil {
		writePracticeGenerationError(
			w, err, practice.ErrGuidanceExhausted,
			"Could not generate sufficiently new guidance. Try regenerate again.",
		)
		return
	}
	logger.Info("request.success", map[string]any{
		"status": 200, "durationMs": logging.ElapsedMs(started),
		"model": result.Meta.Model, "attempt": result.Meta.Attempt,
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleStudyWords(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.study-words.get", r)
	values := r.URL.Query()
	level, ok := s.practiceEnglishLevel(w, r, values.Get("level"))
	if !ok {
		return
	}
	result, err := s.practiceGenerator.StudyPack(r.Context(), practice.StudyPackInput{
		RefreshToken: values.Get("refresh"), EnglishLevel: level,
		Interests: normalizeURLInterests(values), AvoidWords: values["avoidWord"],
	})
	if err != nil {
		writePracticeGenerationError(
			w, err, practice.ErrStudyPackExhausted,
			"Could not generate a valid words pack. Try regenerate.",
		)
		return
	}
	logger.Info("request.success", map[string]any{
		"status": 200, "durationMs": logging.ElapsedMs(started),
		"model": result.Meta.Model, "attempt": result.Meta.Attempt, "wordsCount": len(result.Words),
	})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) practiceEnglishLevel(w http.ResponseWriter, r *http.Request, requested string) (string, bool) {
	user, err := s.optionalUser(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load session."})
		return "", false
	}
	if user != nil {
		return user.EnglishLevel, true
	}
	return domain.NormalizeEnglishLevel(requested), true
}

func writePracticeGenerationError(w http.ResponseWriter, err error, exhausted error, exhaustedMessage string) {
	if errors.Is(err, exhausted) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": exhaustedMessage})
		return
	}
	var chatErr ai.ChatError
	if errors.As(err, &chatErr) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": chatErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{
		"error": "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service.",
	})
}
