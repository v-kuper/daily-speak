package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/quota"
)

type savedAudioFile struct {
	publicURL    string
	absolutePath string
}

func saveAudioFile(kind string, userID string, id string, audio *domain.ParsedAudioDataURL) (savedAudioFile, error) {
	userDir := domain.SanitizePathSegment(userID)
	fileName := id + "." + audio.Extension
	publicBase := "/uploads/" + kind
	storageRoot := filepath.Join(resolveUploadsDir(), kind)
	directory := filepath.Join(storageRoot, userDir)
	absolutePath := filepath.Join(directory, fileName)
	data, err := domain.DecodeBase64(audio.Base64)
	if err != nil || len(data) <= 0 || len(data) > domain.MaxAudioUploadBytes {
		return savedAudioFile{}, errors.New("audio payload is invalid")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return savedAudioFile{}, err
	}
	if err := os.WriteFile(absolutePath, data, 0o644); err != nil {
		return savedAudioFile{}, err
	}
	return savedAudioFile{publicURL: publicBase + "/" + userDir + "/" + fileName, absolutePath: absolutePath}, nil
}

type quotaHTTPError struct {
	status  int
	message string
}

func (e *quotaHTTPError) Error() string {
	return e.message
}

func recordingQuotaError(q quota.RecordingQuota, duration int) *quotaHTTPError {
	if q.IsSubscriber {
		if duration > domain.SubscriberMaxSessionSeconds {
			return &quotaHTTPError{status: http.StatusBadRequest, message: "Subscribers can save recordings up to 10:00 per session."}
		}
		return nil
	}
	remaining := 0
	if q.WeeklyRemainingSeconds != nil {
		remaining = *q.WeeklyRemainingSeconds
	}
	if duration > remaining {
		return &quotaHTTPError{
			status:  http.StatusForbidden,
			message: "Weekly free limit exceeded. You have " + domain.FormatSeconds(remaining) + " left out of " + domain.FormatSeconds(domain.FreeWeeklyLimitSeconds) + " this week.",
		}
	}
	return nil
}

func recordingQuotaAfterSave(before quota.RecordingQuota, duration int) quota.RecordingQuota {
	after := before
	savedSeconds := domain.ToNonNegativeInt(duration)
	after.WeeklyUsedSeconds = domain.ToNonNegativeInt(before.WeeklyUsedSeconds) + savedSeconds
	if before.WeeklyRemainingSeconds != nil {
		remaining := domain.ToNonNegativeInt(*before.WeeklyRemainingSeconds) - savedSeconds
		if remaining < 0 {
			remaining = 0
		}
		after.WeeklyRemainingSeconds = &remaining
	}
	return after
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func chooseString(condition bool, ifTrue string, ifFalse string) string {
	if condition {
		return ifTrue
	}
	return ifFalse
}
