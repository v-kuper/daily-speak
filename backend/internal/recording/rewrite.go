package recording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/aiparse"
	"daily-speaking-practice/backend/internal/domain"
)

var ErrRewrite = errors.New("The natural English version could not be generated. Please try again later.")

type Rewriter interface {
	Rewrite(context.Context, RewriteInput, AnalysisLogger) (string, error)
}

type RewriteInput struct {
	Transcript   string
	Suggestions  []Suggestion
	EnglishLevel string
}

func (s *AnalysisService) Rewrite(ctx context.Context, input RewriteInput, logger AnalysisLogger) (string, error) {
	if strings.TrimSpace(input.Transcript) == "" {
		return "", ErrRewrite
	}
	seed := absMod(domain.HashString(input.Transcript)*193+domain.HashString(input.EnglishLevel)*29, 2147483647)
	prompt := recordingNaturalVersionPrompt(input.Transcript, input.Suggestions, input.EnglishLevel)
	for attempt := 0; attempt < 2; attempt++ {
		strictJSON := attempt > 0
		content, err := s.provider.Complete(ctx, AnalysisCompletionRequest{
			SystemPrompt: chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You rewrite learner speech as natural conversational English and output JSON only."),
			UserPrompt:   prompt,
			Temperature:  chooseFloat(strictJSON, 0.15, 0.35),
			Seed:         seed + attempt*97,
			StrictJSON:   strictJSON,
		})
		if err != nil {
			logger.Warn("recording.rewrite_request_failed", map[string]any{"errorName": "provider_error"})
			return "", ErrRewrite
		}
		if corrected := parseNaturalTranscriptFromContent(content); corrected != "" {
			return corrected, nil
		}
	}
	return "", ErrRewrite
}

func parseNaturalTranscriptFromContent(content string) string {
	for _, candidate := range aiparse.ExtractJSONCandidates(content) {
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		for _, key := range []string{"correctedTranscript", "naturalTranscript", "improvedTranscript"} {
			raw, ok := payload[key]
			if !ok {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) == nil {
				if normalized := domain.NormalizeTranscript(value); normalized != "" && !containsCyrillic(normalized) {
					return normalized
				}
			}
		}
	}
	return ""
}

type rewriteCorrection struct {
	Wrong string `json:"wrong"`
	Right string `json:"right"`
}

func recordingNaturalVersionPrompt(transcript string, suggestions []Suggestion, englishLevel string) string {
	corrections := make([]rewriteCorrection, 0, len(suggestions))
	for _, item := range suggestions {
		corrections = append(corrections, rewriteCorrection{Wrong: item.Wrong, Right: item.Right})
	}
	suggestionsJSON, _ := json.Marshal(corrections)
	return strings.Join([]string{
		"Learner level: " + domain.FormatEnglishLevel(englishLevel) + ".",
		recordingNaturalVersionLevelGuidance(englishLevel),
		"Rewrite the transcript as natural conversational English while you preserve the speaker's meaning, intent, and factual details.",
		"Replace every Russian word or phrase with its supplied English correction so the result is English-only.",
		"Apply the supplied corrections, fix sentence structure and word order, and remove accidental repetitions or filler that make the thought unclear.",
		"Do not invent new details, opinions, or events. Keep the result achievable and useful for a learner at the stated level.",
		`Return only JSON with this exact shape: {"correctedTranscript":"..."}.`,
		"No markdown and no extra keys.",
		"Corrections: " + string(suggestionsJSON) + ".",
		`Transcript: """` + recordingTranscriptForPrompt(transcript) + `""".`,
	}, " ")
}

func recordingNaturalVersionLevelGuidance(englishLevel string) string {
	switch domain.NormalizeEnglishLevel(englishLevel) {
	case "a1":
		return "Use very simple everyday vocabulary and short spoken sentences."
	case "a2":
		return "Use simple everyday vocabulary and clear spoken sentences."
	case "b2":
		return "Use natural upper-intermediate vocabulary, connectors, and varied spoken sentences."
	case "c1":
		return "Use fluent advanced vocabulary and idiomatic but precise conversational phrasing."
	case "c2":
		return "Use sophisticated near-native vocabulary, nuance, and idiomatic conversational phrasing."
	default:
		return "Use clear intermediate vocabulary and natural spoken sentence structures."
	}
}
