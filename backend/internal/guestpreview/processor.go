package guestpreview

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"daily-speaking-practice/backend/internal/recording"
)

type Job struct {
	ID         string
	ResourceID string
	LeaseToken string
}

type AudioMaterializer interface {
	Materialize(context.Context, string) (string, func(), error)
}

type ProcessingWork struct {
	AudioAssetID       string
	Transcript         string
	DeclaredDuration   int
	InterviewSessionID *string
}

type ProcessingStore interface {
	Claim(context.Context, Job) (ProcessingWork, bool, error)
	SaveTranscript(context.Context, Job, string, int) (bool, error)
	UpdateDuration(context.Context, Job, int) error
	IsActive(context.Context, Job) (bool, error)
	Complete(context.Context, Job, []recording.Suggestion) error
}

type ProcessorDependencies struct {
	Store              ProcessingStore
	Materializer       AudioMaterializer
	ProbeAudioDuration func(context.Context, string) (time.Duration, error)
	Transcribe         func(context.Context, string) (string, error)
	TranscribeTimed    func(context.Context, string) (recording.TimedTranscript, error)
	Analyzer           recording.PreviewAnalyzer
}

type InterviewProcessingStore interface {
	LoadInterviewTurns(context.Context, string) ([]recording.InterviewTurn, error)
	SaveInterviewTranscript(context.Context, Job, string, int, string, map[int]string) (bool, error)
	SealInterviewLastTurn(context.Context, Job, string, int) error
}

type Processor struct{ dependencies ProcessorDependencies }

func NewProcessor(dependencies ProcessorDependencies) *Processor {
	return &Processor{dependencies: dependencies}
}

func (p *Processor) Process(ctx context.Context, job Job) error {
	if p == nil || p.dependencies.Store == nil {
		return errors.New("guest preview processor is not configured")
	}
	work, found, err := p.dependencies.Store.Claim(ctx, job)
	if err != nil || !found {
		return err
	}
	if p.dependencies.Materializer == nil {
		return errors.New("guest preview audio is unavailable")
	}
	path, cleanup, err := p.dependencies.Materializer.Materialize(ctx, work.AudioAssetID)
	if err != nil {
		return errors.New("guest preview audio is unavailable")
	}
	defer cleanup()
	if p.dependencies.ProbeAudioDuration == nil {
		return errors.New("guest preview duration verification is not configured")
	}
	actualDuration, err := p.dependencies.ProbeAudioDuration(ctx, path)
	if err != nil {
		return fmt.Errorf("verify guest preview duration: %w", err)
	}
	if actualDuration <= 0 || actualDuration > MaxDuration {
		return errors.New("guest preview audio exceeds the 60 second limit")
	}
	verifiedSeconds := int(math.Ceil(actualDuration.Seconds()))
	actualMS := int(math.Ceil(float64(actualDuration) / float64(time.Millisecond)))
	if work.InterviewSessionID != nil {
		interviewStore, ok := p.dependencies.Store.(InterviewProcessingStore)
		if !ok {
			return errors.New("guest interview timeline is not configured")
		}
		if err := interviewStore.SealInterviewLastTurn(ctx, job, *work.InterviewSessionID, actualMS); err != nil {
			return err
		}
	}
	transcript := work.Transcript
	if transcript == "" {
		if p.dependencies.Transcribe == nil && p.dependencies.TranscribeTimed == nil {
			return errors.New("guest preview transcription is not configured")
		}
		interviewStore, isInterviewStore := p.dependencies.Store.(InterviewProcessingStore)
		var answers map[int]string
		var timed recording.TimedTranscript
		if work.InterviewSessionID != nil && isInterviewStore && p.dependencies.TranscribeTimed != nil {
			timed, err = p.dependencies.TranscribeTimed(ctx, path)
			if err == nil {
				transcript = timed.Text
			}
		}
		if transcript == "" {
			if p.dependencies.Transcribe == nil {
				return errors.New("guest preview transcription fallback is not configured")
			}
			transcript, err = p.dependencies.Transcribe(ctx, path)
		}
		if err != nil {
			return fmt.Errorf("transcribe guest preview: %w", err)
		}
		transcript = recording.NormalizeTranscript(transcript)
		if transcript == "" {
			return errors.New("guest preview transcription is empty")
		}
		if work.InterviewSessionID != nil && isInterviewStore {
			turns, loadErr := interviewStore.LoadInterviewTurns(ctx, *work.InterviewSessionID)
			if loadErr != nil {
				return loadErr
			}
			answers = recording.FinalInterviewAnswersWithinDuration(turns, timed, actualMS)
			if answers == nil {
				answers = recording.FinalInterviewAnswersFromProvisional(turns, transcript, actualMS)
			}
		}
		var advanced bool
		if work.InterviewSessionID != nil && isInterviewStore {
			advanced, err = interviewStore.SaveInterviewTranscript(ctx, job, transcript, verifiedSeconds, *work.InterviewSessionID, answers)
		} else {
			advanced, err = p.dependencies.Store.SaveTranscript(ctx, job, transcript, verifiedSeconds)
		}
		if err != nil {
			return err
		}
		if !advanced {
			return nil
		}
	} else if verifiedSeconds != work.DeclaredDuration {
		_ = p.dependencies.Store.UpdateDuration(ctx, job, verifiedSeconds)
	}
	active, err := p.dependencies.Store.IsActive(ctx, job)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if p.dependencies.Analyzer == nil {
		return errors.New("guest preview analysis is not configured")
	}
	corrections, err := p.dependencies.Analyzer.PreviewCorrections(ctx, transcript)
	if err != nil {
		return err
	}
	return p.dependencies.Store.Complete(ctx, job, corrections)
}
