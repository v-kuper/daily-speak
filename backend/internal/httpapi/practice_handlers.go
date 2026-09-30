package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/learner"
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
		writeV1Error(w, r, http.StatusBadRequest, "invalid_date", "Query param `date` must be in YYYY-MM-DD format")
		return
	}
	count := 3
	if raw := values.Get("count"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 1 && parsed != 3) {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_question_count", "Query param `count` must be 1 or 3")
			return
		}
		count = parsed
	}

	user, ok := s.optionalAccountUserV1(w, r)
	if !ok {
		return
	}
	level := learner.NormalizeEnglishLevel(values.Get("level"))
	userID := ""
	if user != nil {
		level = user.EnglishLevel
		userID = user.ID
	}
	result, err := s.practiceGenerator.DailyQuestions(r.Context(), practice.DailyQuestionsInput{
		UserID: userID, Count: count,
		DateKey: dateKey, RefreshToken: values.Get("refresh"), EnglishLevel: level,
		Interests: normalizeURLInterests(values), CurrentQuestions: values["current"], AvoidQuestions: values["avoid"],
	})
	if err != nil {
		if errors.Is(err, practice.ErrHistoryUnavailable) {
			writeV1Error(w, r, http.StatusServiceUnavailable, "question_history_unavailable", "Could not load question history")
			return
		}
		writePracticeGenerationError(
			w, r, err, practice.ErrQuestionsExhausted,
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

func (s *Server) handleDismissDailyQuestion(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	var input struct {
		Question string `json:"question"`
	}
	if !decodeIdentityJSON(w, r, &input) {
		return
	}
	err := s.practiceGenerator.DismissQuestion(r.Context(), identity.User.ID, input.Question)
	if errors.Is(err, practice.ErrInvalidQuestion) {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_question", "Question must be between 1 and 300 characters")
		return
	}
	if err != nil {
		writeV1Error(w, r, http.StatusServiceUnavailable, "question_history_unavailable", "Could not save question preference")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		writeV1Error(w, r, http.StatusBadRequest, "missing_topic", "Query param `topic` is required")
		return
	}
	if len([]rune(topic)) > 300 {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_topic", "Topic is too long")
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
			w, r, err, practice.ErrGuidanceExhausted,
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
			w, r, err, practice.ErrStudyPackExhausted,
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
	user, ok := s.optionalAccountUserV1(w, r)
	if !ok {
		return "", false
	}
	if user != nil {
		return user.EnglishLevel, true
	}
	return learner.NormalizeEnglishLevel(requested), true
}

func writePracticeGenerationError(w http.ResponseWriter, r *http.Request, err error, exhausted error, exhaustedMessage string) {
	if errors.Is(err, exhausted) {
		writeV1Error(w, r, http.StatusBadGateway, "generation_exhausted", exhaustedMessage)
		return
	}
	var chatErr ai.ChatError
	if errors.As(err, &chatErr) {
		writeV1Error(w, r, http.StatusBadGateway, "generation_failed", chatErr.Message)
		return
	}
	writeV1Error(w, r, http.StatusBadGateway, "generation_unavailable", "Cannot connect to the configured language model")
}
