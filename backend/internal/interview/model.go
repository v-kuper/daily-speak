package interview

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid           = errors.New("invalid interview request")
	ErrNotFound          = errors.New("interview not found")
	ErrConflict          = errors.New("interview state conflict")
	ErrQuota             = errors.New("guest interview preview unavailable")
	ErrDurationLimit     = errors.New("interview duration limit exhausted")
	ErrNotReady          = errors.New("interview preparation is not ready")
	ErrUnavailable       = errors.New("interview transcription is unavailable")
	ErrSpeechUnavailable = errors.New("interview question speech is unavailable")
)

const (
	StatusPreparing        = "preparing"
	StatusReady            = "ready"
	StatusRecording        = "recording"
	StatusFinalizing       = "finalizing"
	StatusFinalized        = "finalized"
	StatusFailed           = "failed"
	StatusCancelled        = "cancelled"
	JobKind                = "interview.process"
	minQuestionUsefulWords = 1
	maxQuestionUsefulWords = 20
)

type VocabularyItem struct {
	Word        string `json:"word"`
	Translation string `json:"translation"`
}

type Candidate struct {
	QuestionIndex int      `json:"questionIndex,omitempty"`
	ID            string   `json:"id"`
	Question      string   `json:"question"`
	UsefulWords   []string `json:"usefulWords"`
	Source        string   `json:"source,omitempty"`
}

type Turn struct {
	QuestionIndex         int      `json:"questionIndex,omitempty"`
	Seq                   int      `json:"seq"`
	Question              string   `json:"question"`
	QuestionSource        string   `json:"questionSource"`
	AskedAtMs             int      `json:"askedAtMs"`
	EndedAtMs             *int     `json:"endedAtMs,omitempty"`
	ProvisionalTranscript string   `json:"provisionalTranscript,omitempty"`
	TranscriptStatus      string   `json:"transcriptStatus"`
	UsefulWords           []string `json:"usefulWords"`
}

type Session struct {
	OpeningQuestionIndex int              `json:"openingQuestionIndex,omitempty"`
	ID                   string           `json:"id"`
	Status               string           `json:"status"`
	Topic                string           `json:"topic"`
	OpeningQuestion      string           `json:"openingQuestion"`
	OpeningUsefulWords   []string         `json:"openingUsefulWords"`
	UsefulWords          []string         `json:"usefulWords"`
	UsefulVocabulary     []VocabularyItem `json:"usefulVocabulary"`
	Candidates           []Candidate      `json:"candidates"`
	Turns                []Turn           `json:"turns"`
	CurrentTurnSeq       int              `json:"currentTurnSeq"`
	MaxDurationSeconds   int              `json:"maxDurationSeconds"`
	Error                string           `json:"error,omitempty"`
	CreatedAt            time.Time        `json:"createdAt"`
	StartedAt            *time.Time       `json:"-"`
	ExpiresAt            time.Time        `json:"-"`
}

type CreateInput struct {
	OwnerPrincipalID string
	OwnerKind        string
	UserID           string
	Topic            string
	OpeningQuestion  string
	EnglishLevel     string
	Interests        []string
	IdempotencyKey   string
	RequestDigest    string
}

type AdvanceInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	CurrentTurnSeq   int
	NextCandidateID  string
	AtMs             int
	SkipCurrent      bool
}

type SkipTurnInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	TurnSeq          int
	AtMs             int
}

type AttachAudioInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	TurnSeq          int
	AudioAssetID     string
}

type SaveTurnTranscriptInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	TurnSeq          int
	Transcript       string
}

type RealtimeTranscriptionCredential struct {
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expiresAt"`
	WebSocketURL string    `json:"websocketUrl"`
	Model        string    `json:"model"`
	Encoding     string    `json:"encoding"`
	SampleRate   int       `json:"sampleRate"`
}

type RealtimeCredentialIssuer interface {
	IssueRealtimeCredential(context.Context, time.Duration) (RealtimeTranscriptionCredential, error)
}

type QuestionSpeechCredential struct {
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Endpoint   string    `json:"endpoint"`
	APIVersion string    `json:"apiVersion"`
	Model      string    `json:"model"`
	VoiceID    string    `json:"voiceId"`
}

type QuestionSpeechCredentialIssuer interface {
	IssueQuestionSpeechCredential(context.Context, time.Duration) (QuestionSpeechCredential, error)
}

type CredentialIssuer interface {
	RealtimeCredentialIssuer
	QuestionSpeechCredentialIssuer
}

type FinalizeInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	EndedAtMs        int
	RecordingID      string
	GuestPreviewID   string
}

type Preparation struct {
	OpeningUsefulWords []string
	Candidate          GuidedQuestion
}

type GuidedQuestion struct {
	Question    string   `json:"question"`
	UsefulWords []string `json:"usefulWords"`
}

type ContextTurn struct {
	Seq        int
	Question   string
	Transcript string
}

type Generator interface {
	Prepare(ctx context.Context, topic, openingQuestion, level string, interests []string) (Preparation, error)
	Followup(ctx context.Context, topic, level string, history []ContextTurn, avoid []string) (GuidedQuestion, error)
	Refill(ctx context.Context, topic, level string, history []ContextTurn, avoid []string) (GuidedQuestion, error)
}

type AudioMaterializer interface {
	Materialize(ctx context.Context, assetID string) (string, func(), error)
}

type Transcriber interface {
	Transcribe(ctx context.Context, path string) (string, error)
}
