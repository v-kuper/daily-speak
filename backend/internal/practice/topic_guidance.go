package practice

import (
	"context"
	"fmt"
	"strings"

	"daily-speaking-practice/backend/internal/learner"
)

func (s *Service) TopicGuidance(ctx context.Context, input TopicGuidanceInput) (TopicGuidanceResult, error) {
	topic := strings.TrimSpace(input.Topic)
	if topic == "" {
		return TopicGuidanceResult{}, ErrTopicRequired
	}
	if len([]rune(topic)) > 300 {
		return TopicGuidanceResult{}, ErrTopicTooLong
	}
	level := learner.NormalizeEnglishLevel(input.EnglishLevel)
	interests := learner.NormalizeInterests(input.Interests, 10)
	avoidQuestionsRaw := normalizeQuestions(input.AvoidQuestions, 0)
	avoidWordsRaw := normalizeWords(input.AvoidWords)
	avoidQuestions := lowerSet(avoidQuestionsRaw)
	avoidWords := lowerSet(avoidWordsRaw)
	topicQuestion := lowerSet(normalizeQuestions([]string{topic}, 0))
	seed := absMod(
		hashString(strings.ToLower(topic))*131+
			hashString(strings.ToLower(strings.Join(interests, "|")))*17+
			hashString(level)*19+hashString(input.RefreshToken),
		maxSeed,
	)

	for attempt := 0; attempt < maxGenerationAttempts; attempt++ {
		completion, err := s.provider.Complete(ctx, CompletionRequest{
			SystemPrompt: "You generate concise speaking-practice guidance and must follow the output format exactly.",
			UserPrompt:   topicGuidancePrompt(topic, input.RefreshToken, level, interests, avoidQuestionsRaw, avoidWordsRaw),
			Temperature:  chooseFloat(input.RefreshToken != "", 0.7+float64(attempt)*0.08, 0.2+float64(attempt)*0.05),
			Seed:         absMod(seed+(attempt+1)*7919, maxSeed),
		})
		if err != nil {
			return TopicGuidanceResult{}, fmt.Errorf("generate topic guidance: %w", err)
		}
		guidance, ok := parseTopicGuidance(completion.Content)
		if !ok || anyLowerOverlap(guidance.Questions, topicQuestion) ||
			anyLowerOverlap(guidance.Questions, avoidQuestions) || anyLowerOverlap(guidance.Words, avoidWords) {
			continue
		}
		guidance.Meta = GenerationMeta{Model: completion.Model, Attempt: attempt + 1}
		return guidance, nil
	}
	return TopicGuidanceResult{}, ErrGuidanceExhausted
}
