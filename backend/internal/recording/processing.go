package recording

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/quota"
)

const (
	ProcessingTimeout = 30 * time.Minute
)

type ProcessingJob struct {
	ID         string
	ResourceID string
	LeaseToken string
}

type ProcessingWork struct {
	AnalysisPipeline     string
	UserID               string
	Stage                string
	AudioPath            *string
	AudioAssetID         *string
	Transcript           string
	Suggestions          []Suggestion
	Topic                string
	PracticeType         string
	PhotoObject          *string
	EnglishLevel         string
	PromotedGuestPreview bool
	InterviewSessionID   *string
	DeclaredDuration     int
}

type ProcessingRepository interface {
	LoadProcessingWork(context.Context, ProcessingJob) (ProcessingWork, bool, error)
	VerifyDuration(context.Context, ProcessingJob, int) error
	UserInterests(context.Context, string) ([]string, error)
	SaveTranscript(context.Context, ProcessingJob, string) (bool, error)
	SaveAnalysis(context.Context, ProcessingJob, AnalysisResult) (bool, error)
	CompleteRecording(context.Context, ProcessingJob, RewriteResult, string) (bool, error)
}

type AudioMaterializer interface {
	Materialize(context.Context, string) (string, func(), error)
}

type ProcessingDependencies struct {
	Repository         ProcessingRepository
	Materializer       AudioMaterializer
	ProbeAudioDuration func(context.Context, string) (time.Duration, error)
	Transcribe         func(context.Context, string) (string, error)
	Analyzer           Analyzer
	Rewriter           Rewriter
	NewID              func() string
}

// InterviewProcessingRepository loads the complete stored timeline, including
// skipped turns, and makes saving the answered transcript and its question
// alignment one fenced, atomic state transition.
type InterviewProcessingRepository interface {
	LoadInterviewTimeline(context.Context, string) ([]InterviewTurn, error)
	SaveInterviewTranscript(context.Context, ProcessingJob, string, string, map[int]string) (bool, error)
	VerifyInterviewDuration(context.Context, ProcessingJob, string, int, int) error
}

type Processor struct {
	dependencies ProcessingDependencies
}

func NewProcessor(dependencies ProcessingDependencies) *Processor {
	return &Processor{dependencies: dependencies}
}

func (p *Processor) Process(ctx context.Context, job ProcessingJob, logger AnalysisLogger) error {
	if p == nil || p.dependencies.Repository == nil {
		return errors.New("recording processor is not configured")
	}
	work, found, err := p.dependencies.Repository.LoadProcessingWork(ctx, job)
	if err != nil || !found {
		return err
	}
	cleanupAudio := func() {}
	if work.Stage == "transcribing" {
		if work.AudioAssetID != nil && p.dependencies.Materializer != nil {
			path, cleanup, materializeErr := p.dependencies.Materializer.Materialize(ctx, *work.AudioAssetID)
			if materializeErr == nil {
				work.AudioPath = &path
				cleanupAudio = cleanup
			}
			err = materializeErr
		} else {
			err = errors.New("recording audio is unavailable")
		}
		if err != nil {
			return errors.New("recording audio is unavailable")
		}
	}
	defer cleanupAudio()

	if work.Stage == "transcribing" {
		if p.dependencies.ProbeAudioDuration == nil {
			return errors.New("recording audio duration could not be verified")
		}
		actualDuration, probeErr := p.dependencies.ProbeAudioDuration(ctx, valueOrEmpty(work.AudioPath))
		if probeErr != nil {
			return errors.New("recording audio duration could not be verified")
		}
		actualSeconds := int(math.Ceil(actualDuration.Seconds()))
		if actualDuration <= 0 || actualDuration > time.Duration(quota.AccountMaxSessionSeconds)*time.Second || actualSeconds < 1 {
			return errors.New("recording audio exceeds its duration limit")
		}
		if work.PromotedGuestPreview && actualDuration > time.Duration(quota.GuestMaxSessionSeconds)*time.Second {
			return errors.New("guest preview audio exceeds the 180 second limit")
		}
		actualMS := int(math.Ceil(float64(actualDuration) / float64(time.Millisecond)))
		if work.InterviewSessionID != nil {
			interviewRepository, ok := p.dependencies.Repository.(InterviewProcessingRepository)
			if !ok {
				return errors.New("interview duration verification is not configured")
			}
			if err := interviewRepository.VerifyInterviewDuration(ctx, job, *work.InterviewSessionID, actualSeconds, actualMS); err != nil {
				return err
			}
		} else if err := p.dependencies.Repository.VerifyDuration(ctx, job, actualSeconds); err != nil {
			return err
		}
	}
	return p.resume(ctx, job, work, logger)
}

func (p *Processor) resume(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	switch work.Stage {
	case "transcribing":
		return p.transcribe(ctx, job, work, logger)
	case "suggestions":
		return p.analyze(ctx, job, work, logger)
	case "rewriting":
		return p.rewrite(ctx, job, work, logger)
	default:
		return errors.New("recording processing cannot resume from persisted stage " + strings.TrimSpace(work.Stage))
	}
}

func (p *Processor) transcribe(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	interests, err := p.dependencies.Repository.UserInterests(ctx, work.UserID)
	if err != nil {
		return err
	}
	var transcript string
	var answers map[int]string
	var dialogue []InterviewDialogueTurn
	interviewRepository, isInterviewRepository := p.dependencies.Repository.(InterviewProcessingRepository)
	if work.InterviewSessionID != nil {
		if !isInterviewRepository {
			return errors.New("interview transcript composition is not configured")
		}
		turns, loadErr := interviewRepository.LoadInterviewTimeline(ctx, *work.InterviewSessionID)
		if loadErr != nil {
			return loadErr
		}
		composed, composeErr := ComposeInterviewTranscript(turns)
		if composeErr != nil {
			return composeErr
		}
		transcript, answers, dialogue = composed.Text, composed.Answers, composed.Dialogue
	} else {
		if p.dependencies.Transcribe == nil {
			return errors.New("recording transcription is not configured")
		}
		transcript, err = p.dependencies.Transcribe(ctx, valueOrEmpty(work.AudioPath))
		if err != nil {
			return err
		}
	}
	transcript = NormalizeTranscript(transcript)
	if transcript == "" {
		return errors.New("Transcription returned an empty transcript. Try speaking louder or recording again.")
	}
	var advanced bool
	if work.InterviewSessionID != nil && isInterviewRepository {
		advanced, err = interviewRepository.SaveInterviewTranscript(ctx, job, transcript, *work.InterviewSessionID, answers)
	} else {
		advanced, err = p.dependencies.Repository.SaveTranscript(ctx, job, transcript)
	}
	if err != nil || !advanced {
		return err
	}
	work.Transcript = transcript
	return p.analyzeWithInterests(ctx, job, work, interests, dialogue, logger)
}

func (p *Processor) analyze(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	interests, err := p.dependencies.Repository.UserInterests(ctx, work.UserID)
	if err != nil {
		return err
	}
	dialogue, err := p.interviewDialogue(ctx, work)
	if err != nil {
		return err
	}
	return p.analyzeWithInterests(ctx, job, work, interests, dialogue, logger)
}

func (p *Processor) interviewDialogue(ctx context.Context, work ProcessingWork) ([]InterviewDialogueTurn, error) {
	if work.InterviewSessionID == nil {
		return nil, nil
	}
	repository, ok := p.dependencies.Repository.(InterviewProcessingRepository)
	if !ok {
		return nil, errors.New("interview transcript composition is not configured")
	}
	turns, err := repository.LoadInterviewTimeline(ctx, *work.InterviewSessionID)
	if err != nil {
		return nil, err
	}
	composed, err := ComposeInterviewTranscript(turns)
	if err != nil {
		return nil, err
	}
	if composed.Text != NormalizeTranscript(work.Transcript) {
		return nil, errors.New("persisted interview transcript does not match finalized answers")
	}
	return composed.Dialogue, nil
}

func (p *Processor) analyzeWithInterests(ctx context.Context, job ProcessingJob, work ProcessingWork, interests []string, dialogue []InterviewDialogueTurn, logger AnalysisLogger) error {
	if p.dependencies.Analyzer == nil {
		return errors.New("recording analysis is not configured")
	}
	input := AnalysisInput{
		Pipeline: work.AnalysisPipeline, RecordingID: job.ResourceID, Transcript: work.Transcript, Topic: work.Topic,
		Interests: interests, PracticeType: work.PracticeType, PhotoObject: work.PhotoObject,
		EnglishLevel: work.EnglishLevel, InterviewTurns: dialogue,
	}
	if repository, ok := p.dependencies.Repository.(AnalysisCheckpointRepository); ok {
		input.Checkpoint = repository.AnalysisCheckpoint(job, input)
	}
	analysis, err := p.dependencies.Analyzer.Analyze(ctx, input, logger)
	if err != nil {
		return err
	}
	advanced, err := p.dependencies.Repository.SaveAnalysis(ctx, job, analysis)
	if err != nil || !advanced {
		return err
	}
	work.Suggestions = analysis.Suggestions
	return p.rewrite(ctx, job, work, logger)
}

func (p *Processor) rewrite(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	if p.dependencies.Rewriter == nil {
		return errors.New("recording rewrite is not configured")
	}
	dialogue, err := p.interviewDialogue(ctx, work)
	if err != nil {
		return err
	}
	corrected, err := p.dependencies.Rewriter.Rewrite(ctx, RewriteInput{
		Transcript: work.Transcript, Suggestions: work.Suggestions, EnglishLevel: work.EnglishLevel,
		InterviewTurns: dialogue,
	}, logger)
	if err != nil {
		return err
	}
	shadowingJobID := ""
	if p.dependencies.NewID != nil {
		shadowingJobID = p.dependencies.NewID()
	}
	if strings.TrimSpace(shadowingJobID) == "" {
		return errors.New("recording ID generator is not configured")
	}
	_, err = p.dependencies.Repository.CompleteRecording(ctx, job, corrected, shadowingJobID)
	return err
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
