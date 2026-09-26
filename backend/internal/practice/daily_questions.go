package practice

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

var (
	dateKeyPattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	nonDigitPattern = regexp.MustCompile(`\D`)
)

func (s *Service) DailyQuestions(ctx context.Context, input DailyQuestionsInput) (DailyQuestionsResult, error) {
	if !dateKeyPattern.MatchString(input.DateKey) {
		return DailyQuestionsResult{}, ErrInvalidDateKey
	}
	level := domain.NormalizeEnglishLevel(input.EnglishLevel)
	interests := domain.NormalizeInterests(input.Interests, 10)
	avoidQuestions := normalizeQuestions(input.AvoidQuestions, dailyQuestionsCount)
	avoidLower := lowerSet(avoidQuestions)

	dateSeed, _ := strconv.Atoi(strings.ReplaceAll(input.DateKey, "-", ""))
	refreshSeed := 0
	hasRefreshSeed := false
	if input.RefreshToken != "" {
		if parsed, err := strconv.Atoi(nonDigitPattern.ReplaceAllString(input.RefreshToken, "")); err == nil {
			refreshSeed = parsed
			hasRefreshSeed = true
		}
	}
	seed := absMod(
		dateSeed*131+domain.HashString(strings.ToLower(strings.Join(interests, "|")))*17+
			domain.HashString(level)*19+refreshSeed,
		maxSeed,
	)

	for attempt := 0; attempt < maxGenerationAttempts; attempt++ {
		completion, err := s.provider.Complete(ctx, CompletionRequest{
			SystemPrompt: "You are an assistant that generates concise speaking-practice questions and always follows output format exactly.",
			UserPrompt:   dailyQuestionsPrompt(input.DateKey, input.RefreshToken, level, interests, avoidQuestions),
			Temperature:  chooseFloat(hasRefreshSeed, 0.7+float64(attempt)*0.08, 0.2+float64(attempt)*0.05),
			Seed:         absMod(seed+(attempt+1)*9973, maxSeed),
		})
		if err != nil {
			return DailyQuestionsResult{}, fmt.Errorf("generate daily questions: %w", err)
		}
		questions, ok := parseQuestions(completion.Content, dailyQuestionsCount)
		if !ok || anyLowerOverlap(questions, avoidLower) {
			continue
		}
		return DailyQuestionsResult{
			Questions: questions,
			Meta:      GenerationMeta{Model: completion.Model, Attempt: attempt + 1},
		}, nil
	}
	return DailyQuestionsResult{}, ErrQuestionsExhausted
}
