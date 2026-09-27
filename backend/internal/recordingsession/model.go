package recordingsession

import "time"

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

type Recording struct {
	ID                  string
	Topic               string
	Duration            int
	Timestamp           time.Time
	Status              string
	Transcript          string
	CorrectedTranscript string
	SuggestionsJSON     []byte
	ProcessingStage     *string
	PracticeType        string
	AudioDataURL        *string
	PhotoDataURL        *string
	PhotoObject         *string
	ProcessingError     *string
	ShadowingStatus     string
	ShadowingAudioURL   *string
	ShadowingError      *string
	ShadowingUpdatedAt  time.Time
}

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
