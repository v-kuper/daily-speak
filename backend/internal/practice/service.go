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
	ErrInvalidDateKey       = errors.New("date must use YYYY-MM-DD format")
	ErrInvalidQuestionCount = errors.New("question count must be 1 or 3")
	ErrInvalidQuestion      = errors.New("question must be between 1 and 300 characters")
	ErrHistoryUnavailable   = errors.New("question history is unavailable")
	ErrTopicRequired        = errors.New("topic is required")
	ErrTopicTooLong         = errors.New("topic is too long")
	ErrQuestionsExhausted   = errors.New("could not generate sufficiently new questions")
	ErrGuidanceExhausted    = errors.New("could not generate sufficiently new guidance")
	ErrStudyPackExhausted   = errors.New("could not generate a valid study pack")
)

type Generator interface {
	DailyQuestions(context.Context, DailyQuestionsInput) (DailyQuestionsResult, error)
	DismissQuestion(context.Context, string, string) error
	TopicGuidance(context.Context, TopicGuidanceInput) (TopicGuidanceResult, error)
	StudyPack(context.Context, StudyPackInput) (StudyPackResult, error)
}

// QuestionHistory is the persistence port for answered and explicitly dismissed questions.
type QuestionHistory interface {
	ListAvoidQuestions(context.Context, string) ([]string, error)
	DismissQuestion(context.Context, string, string, string) error
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
	UserID           string
	DateKey          string
	Count            int
	RefreshToken     string
	EnglishLevel     string
	Interests        []string
	CurrentQuestions []string
	AvoidQuestions   []string
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
	history  QuestionHistory
}

func NewService(provider CompletionProvider, history ...QuestionHistory) *Service {
	service := &Service{provider: provider}
	if len(history) > 0 {
		service.history = history[0]
	}
	return service
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
