package recording

import (
	"context"
	"errors"
	"strings"
	"time"
)

const StrengthsTimeout = 2 * time.Minute

var ErrStrengthsUnavailable = errors.New("Good examples can be retried after correction analysis is complete.")

type StrengthAnalyzer interface {
	AnalyzeStrengths(context.Context, AnalysisInput, []Suggestion, AnalysisLogger) ([]Strength, error)
}

type StrengthsRepository interface {
	LoadStrengthsWork(context.Context, ProcessingJob) (ProcessingWork, bool, error)
	SaveStrengths(context.Context, ProcessingJob, []Strength) error
}

type StrengthsRetrySource struct {
	Status          string
	Stage           string
	StrengthsStatus string
	Transcript      string
}

type StrengthsTransaction interface {
	LockOwned(context.Context, string, string) (StrengthsRetrySource, bool, error)
	Enqueue(context.Context, string, string) error
}

type StrengthsUnitOfWork interface {
	ExecuteStrengths(context.Context, func(StrengthsTransaction) error) error
}

type StrengthsService struct {
	unit       StrengthsUnitOfWork
	repository StrengthsRepository
	analyzer   StrengthAnalyzer
	newID      func() string
}

func NewStrengthsService(unit StrengthsUnitOfWork, repository StrengthsRepository, analyzer StrengthAnalyzer, newID func() string) *StrengthsService {
	return &StrengthsService{unit: unit, repository: repository, analyzer: analyzer, newID: newID}
}

func (s *StrengthsService) Retry(ctx context.Context, owner, recordingID string) (bool, error) {
	if strings.TrimSpace(recordingID) == "" {
		return false, ErrNotFound
	}
	if s == nil || s.unit == nil || s.newID == nil {
		return false, errors.New("strengths retry service is not configured")
	}
	scheduled := false
	err := s.unit.ExecuteStrengths(ctx, func(tx StrengthsTransaction) error {
		source, found, err := tx.LockOwned(ctx, owner, recordingID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if source.StrengthsStatus == "processing" || source.StrengthsStatus == "ready" {
			return nil
		}
		if strings.TrimSpace(source.Transcript) == "" || (source.Status != "ready" && source.Stage != "rewriting") {
			return ErrStrengthsUnavailable
		}
		if err := tx.Enqueue(ctx, recordingID, s.newID()); err != nil {
			return err
		}
		scheduled = true
		return nil
	})
	return scheduled, err
}

func (s *StrengthsService) Process(ctx context.Context, job ProcessingJob, logger AnalysisLogger) error {
	if s == nil || s.repository == nil || s.analyzer == nil {
		return errors.New("strengths processor is not configured")
	}
	work, found, err := s.repository.LoadStrengthsWork(ctx, job)
	if err != nil || !found {
		return err
	}
	var dialogue []InterviewDialogueTurn
	if work.InterviewSessionID != nil {
		repository, ok := s.repository.(InterviewProcessingRepository)
		if !ok {
			return errors.New("interview transcript composition is not configured")
		}
		turns, err := repository.LoadInterviewTimeline(ctx, *work.InterviewSessionID)
		if err != nil {
			return err
		}
		composed, err := ComposeInterviewTranscript(turns)
		if err != nil {
			return err
		}
		if composed.Text != work.Transcript {
			return errors.New("persisted interview transcript does not match finalized answers")
		}
		dialogue = composed.Dialogue
	}
	strengths, err := s.analyzer.AnalyzeStrengths(ctx, AnalysisInput{RecordingID: job.ResourceID,
		Transcript: work.Transcript, InterviewTurns: dialogue, Topic: work.Topic,
		PracticeType: work.PracticeType, PhotoObject: work.PhotoObject, EnglishLevel: work.EnglishLevel}, work.Suggestions, logger)
	if err != nil {
		return err
	}
	return s.repository.SaveStrengths(ctx, job, strengths)
}
