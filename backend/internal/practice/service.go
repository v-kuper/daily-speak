package practice

import (
	"context"
	"errors"
)

const (
	dailyQuestionsCount       = 3
	topicGuidanceQuestionsCnt = 10
	topicGuidanceWordsCnt     = 8
	maxGenerationAttempts     = 3
	maxSeed                   = 2147483647
)

var (
	ErrInvalidDateKey     = errors.New("date must use YYYY-MM-DD format")
	ErrTopicRequired      = errors.New("topic is required")
	ErrTopicTooLong       = errors.New("topic is too long")
	ErrQuestionsExhausted = errors.New("could not generate sufficiently new questions")
	ErrGuidanceExhausted  = errors.New("could not generate sufficiently new guidance")
	ErrStudyPackExhausted = errors.New("could not generate a valid study pack")
)

type Generator interface {
	DailyQuestions(context.Context, DailyQuestionsInput) (DailyQuestionsResult, error)
	TopicGuidance(context.Context, TopicGuidanceInput) (TopicGuidanceResult, error)
	StudyPack(context.Context, StudyPackInput) (StudyPackResult, error)
}

// CompletionProvider is the outbound port used by the practice application
// service. Provider-specific request formats and settings belong in adapters.
type CompletionProvider interface {
	Complete(context.Context, CompletionRequest) (Completion, error)
}

type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float64
	Seed         int
}

type Completion struct {
	Content string
	Model   string
}

type DailyQuestionsInput struct {
	DateKey        string
	RefreshToken   string
	EnglishLevel   string
	Interests      []string
	AvoidQuestions []string
}

type TopicGuidanceInput struct {
	Topic          string
	RefreshToken   string
	EnglishLevel   string
	Interests      []string
	AvoidQuestions []string
	AvoidWords     []string
}

type StudyPackInput struct {
	RefreshToken string
	EnglishLevel string
	Interests    []string
	AvoidWords   []string
}

type GenerationMeta struct {
	Model   string
	Attempt int
}

type DailyQuestionsResult struct {
	Questions []string       `json:"questions"`
	Meta      GenerationMeta `json:"-"`
}

type TopicGuidanceResult struct {
	Questions []string       `json:"questions"`
	Words     []string       `json:"words"`
	Meta      GenerationMeta `json:"-"`
}

type StudyPackResult struct {
	Words []string       `json:"words"`
	Text  string         `json:"text"`
	Meta  GenerationMeta `json:"-"`
}

type Service struct {
	provider CompletionProvider
}

func NewService(provider CompletionProvider) *Service {
	return &Service{provider: provider}
}

func chooseFloat(condition bool, ifTrue float64, ifFalse float64) float64 {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func absMod(value int, mod int) int {
	if mod <= 0 {
		return value
	}
	out := value % mod
	if out < 0 {
		return -out
	}
	return out
}
