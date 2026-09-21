package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
)

const defaultAnalysisConcurrency = 3

var errRecordingAnalysis = errors.New("AI suggestions could not be generated. Please try again later.")

type detectorPassResult struct {
	candidates []analysisCandidate
	err        error
}

func analysisConcurrency() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AI_ANALYSIS_CONCURRENCY")))
	if err != nil || value < 1 || value > len(recordingAnalysisPasses) {
		return defaultAnalysisConcurrency
	}
	return value
}

func (s *Server) generateRecordingSuggestions(ctx context.Context, recordingID string, transcript string, topic string, interests []string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) ([]suggestion, error) {
	transcriptForPrompt := recordingTranscriptForPrompt(transcript)
	if transcriptForPrompt == "" {
		return []suggestion{}, nil
	}
	input := recordingAnalysisInput{
		Transcript:   transcriptForPrompt,
		Topic:        strings.TrimSpace(topic),
		Interests:    interests,
		PracticeType: practiceType,
		PhotoObject:  photoObject,
		EnglishLevel: domain.FormatEnglishLevel(englishLevel),
		Russian:      extractRussianPhrases(transcriptForPrompt),
	}
	settings := ai.ResolveSettingsForUser()
	candidates, err := runRecordingDetectors(ctx, s.aiClient, input, settings, analysisConcurrency(), recordingID, logger)
	if err != nil {
		return nil, errRecordingAnalysis
	}
	suggestions, err := requestReviewedSuggestions(ctx, s.aiClient, settings, transcriptForPrompt, candidates, input.Russian, recordingID, logger)
	if err != nil {
		return nil, errRecordingAnalysis
	}
	return suggestions, nil
}

func runRecordingDetectors(ctx context.Context, client ai.ChatClient, input recordingAnalysisInput, settings ai.Settings, concurrency int, recordingID string, logger logging.Logger) ([]analysisCandidate, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(recordingAnalysisPasses) {
		concurrency = len(recordingAnalysisPasses)
	}
	results := make([]detectorPassResult, len(recordingAnalysisPasses))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case index, open := <-jobs:
					if !open {
						return
					}
					candidates, err := requestDetectorCandidates(ctx, client, settings, recordingAnalysisPasses[index], input, index, recordingID, logger)
					results[index] = detectorPassResult{candidates: candidates, err: err}
				}
			}
		}()
	}

	queuedAll := true
	for index := range recordingAnalysisPasses {
		select {
		case jobs <- index:
		case <-ctx.Done():
			queuedAll = false
		}
		if !queuedAll {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if !queuedAll || ctx.Err() != nil {
		return nil, ctx.Err()
	}

	merged := []analysisCandidate{}
	for index, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		sort.SliceStable(result.candidates, func(i, j int) bool {
			iPosition := strings.Index(input.Transcript, result.candidates[i].Wrong)
			jPosition := strings.Index(input.Transcript, result.candidates[j].Wrong)
			if iPosition != jPosition {
				return iPosition < jPosition
			}
			return result.candidates[i].Wrong < result.candidates[j].Wrong
		})
		for candidateIndex := range result.candidates {
			result.candidates[candidateIndex].PassIndex = index
		}
		merged = append(merged, result.candidates...)
	}

	counts := map[suggestionCategory]int{}
	for index := range merged {
		counts[merged[index].Category]++
		merged[index].ID = fmt.Sprintf("%s-%03d", merged[index].Category, counts[merged[index].Category])
	}
	return merged, nil
}

func requestDetectorCandidates(ctx context.Context, client ai.ChatClient, settings ai.Settings, pass analysisPass, input recordingAnalysisInput, passIndex int, recordingID string, logger logging.Logger) ([]analysisCandidate, error) {
	prompt := recordingDetectorPrompt(pass, input)
	for attempt := 0; attempt < 2; attempt++ {
		strictJSON := attempt > 0
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You identify one specified category of genuine English learner errors and output JSON only.")},
				{"role": "user", "content": prompt},
			},
			"options": map[string]any{
				"temperature": chooseFloat(strictJSON, 0.05, 0.2),
				"seed": absMod(
					domain.HashString(input.Transcript)*17+domain.HashString(string(pass.Category))*31+attempt*97,
					2147483647,
				),
			},
		}
		if !settings.IsThinkingModel {
			body["format"] = "json"
		}
		payload, err := client.PostChat(ctx, body)
		if err != nil {
			continue
		}
		candidates, valid := parseDetectorCandidates(ai.ExtractMessageContent(payload), pass.Category, input.Transcript)
		if valid {
			for index := range candidates {
				candidates[index].PassIndex = passIndex
			}
			return candidates, nil
		}
	}
	return nil, errRecordingAnalysis
}

func requestReviewedSuggestions(ctx context.Context, client ai.ChatClient, settings ai.Settings, transcript string, candidates []analysisCandidate, requiredRussian []string, recordingID string, logger logging.Logger) ([]suggestion, error) {
	prompt := recordingReviewerPrompt(transcript, candidates, requiredRussian)
	for attempt := 0; attempt < 2; attempt++ {
		strictJSON := attempt > 0
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You adjudicate supplied learner-error candidates and output JSON only.")},
				{"role": "user", "content": prompt},
			},
			"options": map[string]any{
				"temperature": chooseFloat(strictJSON, 0.05, 0.2),
				"seed":        absMod(domain.HashString(transcript)*193+attempt*97, 2147483647),
			},
		}
		if !settings.IsThinkingModel {
			body["format"] = "json"
		}
		payload, err := client.PostChat(ctx, body)
		if err != nil {
			continue
		}
		suggestions, valid := parseReviewedSuggestions(ai.ExtractMessageContent(payload), transcript, candidates, requiredRussian)
		if valid {
			return suggestions, nil
		}
	}
	return nil, errRecordingAnalysis
}
