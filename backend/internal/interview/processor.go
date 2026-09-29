package interview

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/workqueue"
)

type ProcessingStore interface {
	LoadPreparation(context.Context, string) (PreparationWork, bool, error)
	SavePreparation(context.Context, workqueue.Job, string, Preparation) error
	LoadTurn(context.Context, string) (TurnWork, bool, error)
	LoadRefill(context.Context, string) (RefillWork, bool, error)
	SaveTranscript(context.Context, workqueue.Job, string, string) (string, error)
	SaveAdaptive(context.Context, workqueue.Job, string, int, GuidedQuestion) (bool, error)
	SaveRefill(context.Context, workqueue.Job, string, GuidedQuestion) (int, error)
	QueueWaitMs(context.Context, string) int64
}

type TranscribeFunc func(context.Context, string) (string, error)

func (f TranscribeFunc) Transcribe(ctx context.Context, path string) (string, error) {
	return f(ctx, path)
}

type Processor struct {
	store        ProcessingStore
	materializer AudioMaterializer
	transcriber  Transcriber
	generator    Generator
}

func NewProcessor(store ProcessingStore, materializer AudioMaterializer, transcriber Transcriber, generator Generator) *Processor {
	return &Processor{store: store, materializer: materializer, transcriber: transcriber, generator: generator}
}

func (p *Processor) Process(ctx context.Context, job workqueue.Job) error {
	if p == nil || p.store == nil || p.generator == nil {
		return errors.New("interview processor is not configured")
	}
	var payload struct {
		Step string `json:"step"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	logger := logging.ForBackground("worker.interview")
	queueWait := p.store.QueueWaitMs(ctx, job.ID)
	switch payload.Step {
	case "prepare":
		work, required, err := p.store.LoadPreparation(ctx, job.ResourceID)
		if err != nil || !required {
			return err
		}
		started := time.Now()
		prepared, err := p.generator.Prepare(ctx, work.Topic, work.OpeningQuestion, work.EnglishLevel, work.Interests)
		if err != nil {
			logger.Warn("prepare.failed", map[string]any{"queueWaitMs": queueWait, "generationMs": logging.ElapsedMs(started)})
			return err
		}
		if err := p.store.SavePreparation(ctx, job, work.SessionID, prepared); err != nil {
			return err
		}
		logger.Info("prepare.completed", map[string]any{"queueWaitMs": queueWait, "generationMs": logging.ElapsedMs(started), "candidateCount": 1})
		return nil
	case "turn":
		work, required, err := p.store.LoadTurn(ctx, job.ResourceID)
		if err != nil || !required {
			return err
		}
		transcriptionMs := int64(0)
		if work.Status != "ready" {
			if p.materializer == nil || p.transcriber == nil {
				return errors.New("interview transcription is not configured")
			}
			started := time.Now()
			path, cleanup, err := p.materializer.Materialize(ctx, work.AudioAssetID)
			if err != nil {
				return err
			}
			defer cleanup()
			transcript, err := p.transcriber.Transcribe(ctx, path)
			transcriptionMs = logging.ElapsedMs(started)
			if err != nil {
				logger.Warn("turn.transcription_failed", map[string]any{"queueWaitMs": queueWait, "transcriptionMs": transcriptionMs})
				return err
			}
			canonicalTranscript, err := p.store.SaveTranscript(ctx, job, work.TurnID, transcript)
			if err != nil {
				return err
			}
			work.Transcript = canonicalTranscript
		}
		if len(work.History) > 0 {
			work.History[len(work.History)-1].Transcript = work.Transcript
		}
		if strings.TrimSpace(work.Transcript) == "" || work.SessionStatus != StatusRecording {
			logger.Info("turn.completed", map[string]any{"queueWaitMs": queueWait, "transcriptionMs": transcriptionMs, "adaptiveAdded": false})
			return nil
		}
		started := time.Now()
		guided, err := p.generator.Followup(ctx, work.Topic, work.EnglishLevel, work.History, work.AvoidQuestions)
		generationMs := logging.ElapsedMs(started)
		if err != nil {
			logger.Warn("turn.generation_failed", map[string]any{"queueWaitMs": queueWait, "transcriptionMs": transcriptionMs, "generationMs": generationMs})
			return err
		}
		added, err := p.store.SaveAdaptive(ctx, job, work.SessionID, work.Seq, guided)
		if err != nil {
			return err
		}
		logger.Info("turn.completed", map[string]any{"queueWaitMs": queueWait, "transcriptionMs": transcriptionMs, "generationMs": generationMs, "adaptiveAdded": added})
		return nil
	case "refill":
		work, needed, err := p.store.LoadRefill(ctx, job.ResourceID)
		if err != nil || !needed {
			return err
		}
		started := time.Now()
		guided, err := p.generator.Refill(ctx, work.Topic, work.EnglishLevel, work.History, work.AvoidQuestions)
		if err != nil {
			logger.Warn("refill.failed", map[string]any{"queueWaitMs": queueWait, "generationMs": logging.ElapsedMs(started)})
			return err
		}
		added, err := p.store.SaveRefill(ctx, job, work.SessionID, guided)
		if err != nil {
			return err
		}
		logger.Info("refill.completed", map[string]any{"queueWaitMs": queueWait, "generationMs": logging.ElapsedMs(started), "candidateCount": added})
		return nil
	default:
		return errors.New("unknown interview processing step")
	}
}
