package shadowing

import (
	"errors"
	"time"
)

const (
	JobTimeout     = 2 * time.Minute
	MaxAudioBytes  = 25 * 1024 * 1024
	FailureMessage = "Pronunciation audio could not be generated. Check the Cartesia configuration or try again."
)

var (
	ErrNotFound              = errors.New("shadowing recording not found")
	ErrTranscriptUnavailable = errors.New("corrected transcript is unavailable")
)

type Job struct {
	ID         string
	ResourceID string
	LeaseToken string
}

type Work struct {
	UserID              string
	CorrectedTranscript string
}

type Asset struct {
	ID              string
	OwnerID         string
	StorageDriver   string
	Bucket          string
	ObjectKey       string
	Size            int64
	Checksum        string
	ETag            string
	LegacyPublicURL string
}
