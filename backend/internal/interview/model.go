package interview

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid       = errors.New("invalid interview request")
	ErrNotFound      = errors.New("interview not found")
	ErrConflict      = errors.New("interview state conflict")
	ErrQuota         = errors.New("guest interview preview unavailable")
	ErrDurationLimit = errors.New("interview duration limit exhausted")
	ErrNotReady      = errors.New("interview preparation is not ready")
	ErrUnavailable   = errors.New("interview transcription is unavailable")
)

const (
	StatusPreparing  = "preparing"
	StatusReady      = "ready"
	StatusRecording  = "recording"
	StatusFinalizing = "finalizing"
	StatusFinalized  = "finalized"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	JobKind          = "interview.process"
)

type Candidate struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Source   string `json:"source,omitempty"`
}

type Turn struct {
	Seq                   int    `json:"seq"`
	Question              string `json:"question"`
	QuestionSource        string `json:"questionSource"`
	AskedAtMs             int    `json:"askedAtMs"`
	EndedAtMs             *int   `json:"endedAtMs,omitempty"`
	ProvisionalTranscript string `json:"provisionalTranscript,omitempty"`
	TranscriptStatus      string `json:"transcriptStatus"`
}

type Session struct {
	ID                 string      `json:"id"`
	Status             string      `json:"status"`
	Topic              string      `json:"topic"`
	OpeningQuestion    string      `json:"openingQuestion"`
	UsefulWords        []string    `json:"usefulWords"`
	Candidates         []Candidate `json:"candidates"`
	Turns              []Turn      `json:"turns"`
	CurrentTurnSeq     int         `json:"currentTurnSeq"`
	MaxDurationSeconds int         `json:"maxDurationSeconds"`
	Error              string      `json:"error,omitempty"`
	CreatedAt          time.Time   `json:"createdAt"`
	StartedAt          *time.Time  `json:"-"`
	ExpiresAt          time.Time   `json:"-"`
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

type FinalizeInput struct {
	OwnerPrincipalID string
	SessionID        string
	IdempotencyKey   string
	EndedAtMs        int
	RecordingID      string
	GuestPreviewID   string
}

type Preparation struct {
	Questions []string
	Words     []string
}

type ContextTurn struct {
	Seq        int
	Question   string
	Transcript string
}

type Generator interface {
	Prepare(ctx context.Context, topic, openingQuestion, level string, interests []string) (Preparation, error)
	Followup(ctx context.Context, topic string, history []ContextTurn, avoid []string) (string, error)
	Refill(ctx context.Context, topic string, history []ContextTurn, avoid []string) ([]string, error)
}

type AudioMaterializer interface {
	Materialize(ctx context.Context, assetID string) (string, func(), error)
}

type Transcriber interface {
	Transcribe(ctx context.Context, path string) (string, error)
}
