package recording

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

const (
	ProcessingTimeout       = 30 * time.Minute
	guestPreviewMaxDuration = 60 * time.Second
	accountMaxDuration      = 10 * time.Minute
)

type ProcessingJob struct {
	ID         string
	ResourceID string
	LeaseToken string
}

type ProcessingWork struct {
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
	VerifiedDurationMS   int
}

type ProcessingRepository interface {
	LoadProcessingWork(context.Context, ProcessingJob) (ProcessingWork, bool, error)
	VerifyDuration(context.Context, ProcessingJob, int) error
	UserInterests(context.Context, string) ([]string, error)
	SaveTranscript(context.Context, ProcessingJob, string) (bool, error)
	SaveSuggestions(context.Context, ProcessingJob, []Suggestion) (bool, error)
	CompleteRecording(context.Context, ProcessingJob, string, string) (bool, error)
}

type AudioMaterializer interface {
	Materialize(context.Context, string) (string, func(), error)
}

type ProcessingDependencies struct {
	Repository         ProcessingRepository
	Materializer       AudioMaterializer
	ProbeAudioDuration func(context.Context, string) (time.Duration, error)
	Transcribe         func(context.Context, string) (string, error)
	TranscribeTimed    func(context.Context, string) (TimedTranscript, error)
	Analyzer           Analyzer
	Rewriter           Rewriter
	NewID              func() string
}

// InterviewProcessingRepository makes saving the full transcript and its
// question alignment one fenced, atomic state transition.
type InterviewProcessingRepository interface {
	LoadInterviewTurns(context.Context, string) ([]InterviewTurn, error)
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
		if actualDuration <= 0 || actualDuration > accountMaxDuration || actualSeconds < 1 {
			return errors.New("recording audio exceeds its duration limit")
		}
		if work.PromotedGuestPreview && actualDuration > guestPreviewMaxDuration {
			return errors.New("guest preview audio exceeds the 60 second limit")
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
			work.VerifiedDurationMS = actualMS
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
	if p.dependencies.Transcribe == nil && p.dependencies.TranscribeTimed == nil {
		return errors.New("recording transcription is not configured")
	}
	interests, err := p.dependencies.Repository.UserInterests(ctx, work.UserID)
	if err != nil {
		return err
	}
	var transcript string
	var answers map[int]string
	var timed TimedTranscript
	interviewRepository, isInterviewRepository := p.dependencies.Repository.(InterviewProcessingRepository)
	if work.InterviewSessionID != nil && isInterviewRepository && p.dependencies.TranscribeTimed != nil {
		timed, err = p.dependencies.TranscribeTimed(ctx, valueOrEmpty(work.AudioPath))
		if err == nil {
			transcript = timed.Text
		}
	}
	if transcript == "" {
		if p.dependencies.Transcribe == nil {
			return errors.New("recording transcription fallback is not configured")
		}
		transcript, err = p.dependencies.Transcribe(ctx, valueOrEmpty(work.AudioPath))
	}
	if err != nil {
		return err
	}
	transcript = NormalizeTranscript(transcript)
	if transcript == "" {
		return errors.New("Whisper returned an empty transcript. Try speaking louder or recording again.")
	}
	if work.InterviewSessionID != nil && isInterviewRepository {
		turns, loadErr := interviewRepository.LoadInterviewTurns(ctx, *work.InterviewSessionID)
		if loadErr != nil {
			return loadErr
		}
		answers = FinalInterviewAnswersWithinDuration(turns, timed, work.VerifiedDurationMS)
		if answers == nil {
			answers = FinalInterviewAnswersFromProvisional(turns, transcript, work.VerifiedDurationMS)
		}
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
	return p.analyzeWithInterests(ctx, job, work, interests, logger)
}

func (p *Processor) analyze(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	interests, err := p.dependencies.Repository.UserInterests(ctx, work.UserID)
	if err != nil {
		return err
	}
	return p.analyzeWithInterests(ctx, job, work, interests, logger)
}

func (p *Processor) analyzeWithInterests(ctx context.Context, job ProcessingJob, work ProcessingWork, interests []string, logger AnalysisLogger) error {
	if p.dependencies.Analyzer == nil {
		return errors.New("recording analysis is not configured")
	}
	suggestions, err := p.dependencies.Analyzer.Analyze(ctx, AnalysisInput{
		RecordingID: job.ResourceID, Transcript: work.Transcript, Topic: work.Topic,
		Interests: interests, PracticeType: work.PracticeType, PhotoObject: work.PhotoObject,
		EnglishLevel: work.EnglishLevel,
	}, logger)
	if err != nil {
		return err
	}
	advanced, err := p.dependencies.Repository.SaveSuggestions(ctx, job, suggestions)
	if err != nil || !advanced {
		return err
	}
	work.Suggestions = suggestions
	return p.rewrite(ctx, job, work, logger)
}

func (p *Processor) rewrite(ctx context.Context, job ProcessingJob, work ProcessingWork, logger AnalysisLogger) error {
	if p.dependencies.Rewriter == nil {
		return errors.New("recording rewrite is not configured")
	}
	corrected, err := p.dependencies.Rewriter.Rewrite(ctx, RewriteInput{
		Transcript: work.Transcript, Suggestions: work.Suggestions, EnglishLevel: work.EnglishLevel,
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
