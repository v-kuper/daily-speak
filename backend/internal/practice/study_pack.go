package practice

import (
	"context"
	"fmt"
	"strings"

	"daily-speaking-practice/backend/internal/learner"
)

func (s *Service) StudyPack(ctx context.Context, input StudyPackInput) (StudyPackResult, error) {
	level := learner.NormalizeEnglishLevel(input.EnglishLevel)
	interests := learner.NormalizeInterests(input.Interests, 10)
	avoidWords := normalizeAvoidWords(input.AvoidWords)
	seed := absMod(
		hashString(level)*131+
			hashString(strings.ToLower(strings.Join(interests, "|")))*17+
			hashString(strings.ToLower(strings.Join(avoidWords, "|")))*19+
			hashString(input.RefreshToken),
		maxSeed,
	)

	for attempt := 0; attempt < maxGenerationAttempts; attempt++ {
		completion, err := s.provider.Complete(ctx, CompletionRequest{
			SystemPrompt: "You generate level-appropriate vocabulary packs and must follow the JSON output format exactly.",
			UserPrompt:   studyWordsPrompt(level, interests, input.RefreshToken, avoidWords),
			Temperature:  chooseFloat(input.RefreshToken != "", 0.68+float64(attempt)*0.08, 0.22+float64(attempt)*0.05),
			Seed:         absMod(seed+(attempt+1)*9157, maxSeed),
		})
		if err != nil {
			return StudyPackResult{}, fmt.Errorf("generate study pack: %w", err)
		}
		pack, ok := parseStudyPack(completion.Content, avoidWords)
		if !ok {
			continue
		}
		pack.Meta = GenerationMeta{Model: completion.Model, Attempt: attempt + 1}
		return pack, nil
	}
	return StudyPackResult{}, ErrStudyPackExhausted
}
