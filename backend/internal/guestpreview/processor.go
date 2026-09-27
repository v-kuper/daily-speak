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
	AudioAssetID     string
	Transcript       string
	DeclaredDuration int
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
	Analyzer           recording.PreviewAnalyzer
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
	transcript := work.Transcript
	if transcript == "" {
		if p.dependencies.Transcribe == nil {
			return errors.New("guest preview transcription is not configured")
		}
		transcript, err = p.dependencies.Transcribe(ctx, path)
		if err != nil {
			return fmt.Errorf("transcribe guest preview: %w", err)
		}
		transcript = recording.NormalizeTranscript(transcript)
		if transcript == "" {
			return errors.New("guest preview transcription is empty")
		}
		advanced, err := p.dependencies.Store.SaveTranscript(ctx, job, transcript, verifiedSeconds)
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
