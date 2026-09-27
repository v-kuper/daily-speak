package guestpreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/recording"
)

const (
	MaxDuration          = 60 * time.Second
	MaxAudioBytes        = 10 * 1024 * 1024
	Retention            = 24 * time.Hour
	ProcessingTimeout    = 10 * time.Minute
	DefaultQueueCapacity = 100
	RetryAfterSeconds    = 15
)

var (
	ErrNotFound = errors.New("guest preview not found")
	ErrConflict = errors.New("guest preview conflicts with existing data")
	ErrCapacity = errors.New("guest preview capacity is exhausted")
)

type CreateRequest struct {
	AudioAssetID string `json:"audioAssetId"`
	Topic        string `json:"topic"`
	Duration     int    `json:"duration"`
	Timestamp    string `json:"timestamp,omitempty"`
	PracticeType string `json:"practiceType"`
}

type Preview struct {
	ID                 string
	GuestPrincipalID   string
	AudioAssetID       string
	Topic              string
	Duration           int
	RecordingTimestamp time.Time
	PracticeType       string
	State              string
	Transcript         string
	PreviewCorrections []recording.Suggestion
	ProcessingError    *string
	PreviewJobID       string
	IdempotencyKey     string
	RequestDigest      string
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NormalizeCreate(input CreateRequest, now time.Time) (CreateRequest, time.Time, error) {
	input.AudioAssetID = strings.TrimSpace(input.AudioAssetID)
	input.Topic = strings.TrimSpace(input.Topic)
	input.PracticeType = strings.ToLower(strings.TrimSpace(input.PracticeType))
	if input.AudioAssetID == "" || len(input.AudioAssetID) > 200 {
		return input, time.Time{}, errors.New("audioAssetId is required")
	}
	if input.Topic == "" || len([]rune(input.Topic)) > 160 {
		return input, time.Time{}, errors.New("topic must contain between 1 and 160 characters")
	}
	if input.Duration < 1 || input.Duration > int(MaxDuration/time.Second) {
		return input, time.Time{}, errors.New("duration must be between 1 and 60 seconds")
	}
	if input.PracticeType != "free_talk" && input.PracticeType != "topic" {
		return input, time.Time{}, errors.New("practiceType must be free_talk or topic")
	}
	timestamp := now
	if strings.TrimSpace(input.Timestamp) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.Timestamp))
		if err != nil {
			return input, time.Time{}, errors.New("timestamp must be an RFC3339 timestamp")
		}
		timestamp = parsed.UTC()
		input.Timestamp = timestamp.Format(time.RFC3339Nano)
	}
	return input, timestamp, nil
}

func RequestDigest(input CreateRequest) string {
	encoded, _ := json.Marshal(struct {
		AudioAssetID string `json:"audioAssetId"`
		Topic        string `json:"topic"`
		Duration     int    `json:"duration"`
		Timestamp    string `json:"timestamp"`
		PracticeType string `json:"practiceType"`
	}{input.AudioAssetID, input.Topic, input.Duration, input.Timestamp, input.PracticeType})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func QueueCapacityFromEnv() int {
	value := strings.TrimSpace(os.Getenv("GUEST_PREVIEW_QUEUE_CAPACITY"))
	if value == "" {
		return DefaultQueueCapacity
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 10000 {
		return DefaultQueueCapacity
	}
	return parsed
}
