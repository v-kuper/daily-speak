package recording

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"daily-speaking-practice/backend/internal/learner"
)

var ErrAnalysis = errors.New("AI suggestions could not be generated. Please try again later.")

type Analyzer interface {
	Analyze(context.Context, AnalysisInput, AnalysisLogger) ([]Suggestion, error)
}

type AnalysisInput struct {
	RecordingID  string
	Transcript   string
	Topic        string
	Interests    []string
	PracticeType string
	PhotoObject  *string
	EnglishLevel string
}

type AnalysisLogger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
}

// AnalysisProvider is the outbound port for model completions. Provider
// request formats and model settings stay in adapter packages.
type AnalysisProvider interface {
	Complete(context.Context, AnalysisCompletionRequest) (string, error)
}

type AnalysisCompletionRequest struct {
	SystemPrompt    string
	UserPrompt      string
	Temperature     float64
	Seed            int
	StrictJSON      bool
	ForceJSON       bool
	DisableThinking bool
}

type AnalysisConfig struct{ Concurrency int }

func AnalysisConfigFromEnv() AnalysisConfig {
	return AnalysisConfig{Concurrency: analysisConcurrencyFromEnv()}
}

type AnalysisService struct {
	provider    AnalysisProvider
	concurrency int
}

func NewAnalysisService(provider AnalysisProvider, config AnalysisConfig) *AnalysisService {
	concurrency := config.Concurrency
	if concurrency < 1 || concurrency > len(recordingAnalysisPasses) {
		concurrency = defaultAnalysisConcurrency
	}
	return &AnalysisService{provider: provider, concurrency: concurrency}
}

func (s *AnalysisService) Analyze(ctx context.Context, request AnalysisInput, logger AnalysisLogger) ([]Suggestion, error) {
	transcript := recordingTranscriptForPrompt(request.Transcript)
	if transcript == "" {
		return []Suggestion{}, nil
	}
	input := recordingAnalysisInput{
		Transcript:   transcript,
		Topic:        strings.TrimSpace(request.Topic),
		Interests:    request.Interests,
		PracticeType: request.PracticeType,
		PhotoObject:  request.PhotoObject,
		EnglishLevel: learner.FormatEnglishLevel(request.EnglishLevel),
		Russian:      extractRussianPhrases(transcript),
	}
	candidates, err := s.runDetectors(ctx, input, request.RecordingID, logger)
	if err != nil {
		return nil, ErrAnalysis
	}
	suggestions, err := s.requestReview(ctx, transcript, candidates, input.Russian, request.RecordingID, logger)
	if err != nil {
		return nil, ErrAnalysis
	}
	return suggestions, nil
}

type detectorPassResult struct {
	candidates []analysisCandidate
	err        error
}

func (s *AnalysisService) runDetectors(ctx context.Context, input recordingAnalysisInput, recordingID string, logger AnalysisLogger) ([]analysisCandidate, error) {
	results := make([]detectorPassResult, len(recordingAnalysisPasses))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < s.concurrency; worker++ {
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
					candidates, err := s.requestDetector(ctx, recordingAnalysisPasses[index], input, index, recordingID, logger)
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

func (s *AnalysisService) requestDetector(ctx context.Context, pass analysisPass, input recordingAnalysisInput, passIndex int, recordingID string, logger AnalysisLogger) ([]analysisCandidate, error) {
	prompt := recordingDetectorPrompt(pass, input)
	for attempt := 0; attempt < 2; attempt++ {
		started := time.Now()
		strictJSON := attempt > 0
		content, err := s.provider.Complete(ctx, AnalysisCompletionRequest{
			SystemPrompt: chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You identify one specified category of genuine English learner errors and output JSON only."),
			UserPrompt:   prompt,
			Temperature:  chooseFloat(strictJSON, 0.05, 0.2),
			Seed:         absMod(hashString(input.Transcript)*17+hashString(string(pass.Category))*31+attempt*97, 2147483647),
			StrictJSON:   strictJSON,
		})
		if err != nil {
			logger.Warn("recording.analysis_detector", analysisLogMeta(recordingID, string(pass.Category), "request_error", attempt+1, time.Since(started), 0))
			continue
		}
		candidates, valid := parseDetectorCandidates(content, pass.Category, input.Transcript)
		if valid {
			for index := range candidates {
				candidates[index].PassIndex = passIndex
			}
			logger.Info("recording.analysis_detector", analysisLogMeta(recordingID, string(pass.Category), "valid", attempt+1, time.Since(started), len(candidates)))
			return candidates, nil
		}
		logger.Warn("recording.analysis_detector", analysisLogMeta(recordingID, string(pass.Category), "invalid_response", attempt+1, time.Since(started), 0))
	}
	return nil, ErrAnalysis
}

func (s *AnalysisService) requestReview(ctx context.Context, transcript string, candidates []analysisCandidate, requiredRussian []string, recordingID string, logger AnalysisLogger) ([]suggestion, error) {
	prompt := recordingReviewerPrompt(transcript, candidates, requiredRussian)
	for attempt := 0; attempt < 2; attempt++ {
		started := time.Now()
		strictJSON := attempt > 0
		content, err := s.provider.Complete(ctx, AnalysisCompletionRequest{
			SystemPrompt: chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You adjudicate supplied learner-error candidates and output JSON only."),
			UserPrompt:   prompt,
			Temperature:  chooseFloat(strictJSON, 0.05, 0.2),
			Seed:         absMod(hashString(transcript)*193+attempt*97, 2147483647),
			StrictJSON:   strictJSON,
		})
		if err != nil {
			logger.Warn("recording.analysis_reviewer", reviewerLogMeta(recordingID, "request_error", attempt+1, time.Since(started), len(candidates), 0))
			continue
		}
		suggestions, valid := parseReviewedSuggestions(content, transcript, candidates, requiredRussian)
		if valid {
			logger.Info("recording.analysis_reviewer", reviewerLogMeta(recordingID, "valid", attempt+1, time.Since(started), len(candidates), len(suggestions)))
			return suggestions, nil
		}
		logger.Warn("recording.analysis_reviewer", reviewerLogMeta(recordingID, "invalid_response", attempt+1, time.Since(started), len(candidates), 0))
	}
	return nil, ErrAnalysis
}

func analysisLogMeta(recordingID, pass, outcome string, attempt int, duration time.Duration, candidateCount int) map[string]any {
	return map[string]any{"recordingId": recordingID, "pass": pass, "attempt": attempt, "durationMs": duration.Milliseconds(), "candidateCount": candidateCount, "outcome": outcome}
}

func reviewerLogMeta(recordingID, outcome string, attempt int, duration time.Duration, inputCount, outputCount int) map[string]any {
	return map[string]any{"recordingId": recordingID, "attempt": attempt, "durationMs": duration.Milliseconds(), "inputCount": inputCount, "outputCount": outputCount, "outcome": outcome}
}
