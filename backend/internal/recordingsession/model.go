package recordingsession

import (
	"time"

	"daily-speaking-practice/backend/internal/recording"
)

type Session struct {
	ID             string
	UserID         string
	Topic          string
	Duration       int
	Timestamp      time.Time
	PracticeType   string
	PhotoDataURL   *string
	PhotoObject    *string
	AudioExtension *string
	ChunkCount     int
	Status         string
	RecordingID    *string
}

type Recording = recording.Record

type StartInput struct {
	Topic        string
	Duration     int
	Timestamp    time.Time
	PracticeType string
	PhotoDataURL string
	PhotoObject  string
}

type StartCommand struct {
	ID           string
	UserID       string
	Topic        string
	Duration     int
	Timestamp    time.Time
	PracticeType string
	PhotoDataURL *string
	PhotoObject  *string
}

type FinalizeInput struct {
	Duration  int
	Timestamp *time.Time
}

type FinalizeCommand struct {
	Session     Session
	RecordingID string
	JobID       string
	Duration    int
	Timestamp   time.Time
	AudioURL    string
}
