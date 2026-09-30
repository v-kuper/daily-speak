package interview

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/workqueue"
)

type AnswerAttempt struct {
	ID              string                     `json:"id"`
	RecordingID     string                     `json:"recordingId"`
	TurnSequence    int                        `json:"turnSequence"`
	Status          string                     `json:"status"`
	Transcript      string                     `json:"transcript"`
	DurationSeconds int                        `json:"durationSeconds"`
	Feedback        *recording.FocusedFeedback `json:"focusedFeedback,omitempty"`
	Error           string                     `json:"error,omitempty"`
	CreatedAt       time.Time                  `json:"createdAt"`
	AudioAssetID    string                     `json:"audioAssetId"`
}
type CreateAttemptInput struct {
	OwnerID, RecordingID, AudioAssetID, IdempotencyKey string
	TurnSeq                                            int
}
type AttemptOriginal struct {
	SessionID, Topic, Question, EnglishLevel string
	Dialogue                                 []recording.InterviewDialogueTurn
}
type AnswerAttemptRepository interface {
	OriginalAnswer(context.Context, string, string, int) (AttemptOriginal, error)
	CreateAttempt(context.Context, CreateAttemptInput) (AnswerAttempt, error)
	GetAttempt(context.Context, string, string, string) (AnswerAttempt, error)
	ListAttempts(context.Context, string, string, int, int, string) ([]AnswerAttempt, error)
	RetryAttempt(context.Context, string, string, string) (AnswerAttempt, error)
}
type AnswerAttemptService struct{ repository AnswerAttemptRepository }

func NewAnswerAttemptService(repository AnswerAttemptRepository) *AnswerAttemptService {
	return &AnswerAttemptService{repository}
}
func (s *AnswerAttemptService) Create(ctx context.Context, input CreateAttemptInput) (AnswerAttempt, error) {
	if s == nil || s.repository == nil {
		return AnswerAttempt{}, ErrUnavailable
	}
	if input.OwnerID == "" || input.RecordingID == "" || input.TurnSeq < 1 || strings.TrimSpace(input.AudioAssetID) == "" || !keyPattern.MatchString(input.IdempotencyKey) {
		return AnswerAttempt{}, ErrInvalid
	}
	if _, err := s.repository.OriginalAnswer(ctx, input.OwnerID, input.RecordingID, input.TurnSeq); err != nil {
		return AnswerAttempt{}, err
	}
	return s.repository.CreateAttempt(ctx, input)
}
func (s *AnswerAttemptService) Get(ctx context.Context, owner, recordingID, id string) (AnswerAttempt, error) {
	if s == nil || s.repository == nil {
		return AnswerAttempt{}, ErrUnavailable
	}
	return s.repository.GetAttempt(ctx, owner, recordingID, id)
}
func (s *AnswerAttemptService) List(ctx context.Context, owner, recordingID string, seq, limit int, beforeID string) ([]AnswerAttempt, error) {
	if s == nil || s.repository == nil {
		return nil, ErrUnavailable
	}
	if limit < 1 || limit > 50 || seq < 1 {
		return nil, ErrInvalid
	}
	if _, err := s.repository.OriginalAnswer(ctx, owner, recordingID, seq); err != nil {
		return nil, err
	}
	return s.repository.ListAttempts(ctx, owner, recordingID, seq, limit, beforeID)
}

type AnswerAttemptWork struct {
	Attempt  AnswerAttempt
	Original AttemptOriginal
	OwnerID  string
}
type AttemptProcessingStore interface {
	LoadAttemptWork(context.Context, workqueue.Job) (AnswerAttemptWork, bool, error)
	SaveAttemptTranscript(context.Context, workqueue.Job, string, int) error
	CompleteAttempt(context.Context, workqueue.Job, *recording.FocusedFeedback) error
	SaveAttemptFeedback(context.Context, workqueue.Job, *recording.FocusedFeedback) error
}
type AttemptAnalyzer interface {
	Analyze(context.Context, recording.AnalysisInput, recording.AnalysisLogger) (recording.AnalysisResult, error)
}
type AnswerAttemptProcessor struct {
	repository   AttemptProcessingStore
	materializer AudioMaterializer
	transcriber  Transcriber
	analyzer     AttemptAnalyzer
	probe        func(context.Context, string) (time.Duration, error)
}

func NewAnswerAttemptProcessor(repository AttemptProcessingStore, materializer AudioMaterializer, transcriber Transcriber, analyzer AttemptAnalyzer, probe func(context.Context, string) (time.Duration, error)) *AnswerAttemptProcessor {
	return &AnswerAttemptProcessor{repository, materializer, transcriber, analyzer, probe}
}
func (p *AnswerAttemptProcessor) Process(ctx context.Context, job workqueue.Job) error {
	work, found, err := p.repository.LoadAttemptWork(ctx, job)
	if err != nil || !found {
		return err
	}
	if work.Attempt.Transcript == "" {
		path, cleanup, err := p.materializer.Materialize(ctx, work.Attempt.AudioAssetID)
		if err != nil {
			return err
		}
		defer cleanup()
		duration, err := p.probe(ctx, path)
		if err != nil {
			return err
		}
		if duration <= 0 || duration > 600*time.Second {
			return ErrDurationLimit
		}
		transcript, err := p.transcriber.Transcribe(ctx, path)
		if err != nil {
			return err
		}
		transcript = strings.Join(strings.Fields(transcript), " ")
		if transcript == "" || len([]rune(transcript)) > 4000 {
			return errors.New("Не удалось распознать ответ. Попробуйте записать его ещё раз.")
		}
		seconds := int((duration + time.Second - 1) / time.Second)
		if err := p.repository.SaveAttemptTranscript(ctx, job, transcript, seconds); err != nil {
			return err
		}
		work.Attempt.Transcript = transcript
	}
	if work.Attempt.Feedback != nil {
		return p.repository.CompleteAttempt(ctx, job, work.Attempt.Feedback)
	}
	result, err := p.analyzer.Analyze(ctx, recording.AnalysisInput{Pipeline: recording.FocusedPipeline, RecordingID: work.Attempt.RecordingID, Transcript: work.Attempt.Transcript, Topic: work.Original.Topic, EnglishLevel: work.Original.EnglishLevel, PracticeType: "topic", InterviewTurns: []recording.InterviewDialogueTurn{{Sequence: work.Attempt.TurnSequence, Question: work.Original.Question, Answer: work.Attempt.Transcript}}, ContextTurns: work.Original.Dialogue, FocusedCheckpoint: &attemptCheckpoint{repository: p.repository, job: job}}, nil)
	if err != nil {
		return err
	}
	return p.repository.CompleteAttempt(ctx, job, result.FocusedFeedback)
}

// The checkpoint is saved before publication so a worker retry reuses paid analysis.
type attemptCheckpoint struct {
	repository AttemptProcessingStore
	job        workqueue.Job
}

func (c *attemptCheckpoint) LoadFocused(ctx context.Context) (*recording.FocusedFeedback, bool, error) {
	work, found, err := c.repository.LoadAttemptWork(ctx, c.job)
	return work.Attempt.Feedback, found && work.Attempt.Feedback != nil, err
}
func (c *attemptCheckpoint) SaveFocused(ctx context.Context, feedback *recording.FocusedFeedback) error {
	return c.repository.SaveAttemptFeedback(ctx, c.job, feedback)
}

func (s *AnswerAttemptService) Retry(ctx context.Context, owner, recordingID, id string) (AnswerAttempt, error) {
	if s == nil || s.repository == nil {
		return AnswerAttempt{}, ErrUnavailable
	}
	return s.repository.RetryAttempt(ctx, owner, recordingID, id)
}
