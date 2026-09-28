package recording

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/quota"
	"github.com/google/uuid"
)

var (
	ErrCreateMediaNotFound         = errors.New("recording media not found")
	ErrCreateIdempotencyConflict   = errors.New("idempotency key was already used with a different request")
	ErrInterviewSessionUnavailable = errors.New("interview session is unavailable")
)

type CreateInput struct {
	Topic              string
	Duration           int
	Timestamp          time.Time
	PracticeType       string
	AudioAssetID       string
	PhotoAssetID       *string
	PhotoObject        *string
	InterviewSessionID *string
}

type Created struct {
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
	PhotoObject         *string
	ProcessingError     *string
	ShadowingStatus     string
	ShadowingError      *string
	ShadowingUpdatedAt  time.Time
	AudioAssetID        string
	PhotoAssetID        *string
	InterviewSessionID  *string
}

type CreateCommand struct {
	PrincipalID   string
	UserID        string
	RecordingID   string
	JobID         string
	RequestDigest string
	Input         CreateInput
}

type CreateTransaction interface {
	LockQuota(context.Context, string, time.Time) (quota.RecordingQuota, error)
	Find(context.Context, string, string) (Created, bool, error)
	LockMedia(context.Context, string, string, string) error
	Insert(context.Context, CreateCommand) (Created, error)
	AttachMedia(context.Context, string, []string) error
	EnqueueProcessing(context.Context, CreateCommand) error
}

type InterviewCreateTransaction interface {
	LinkInterview(context.Context, string, string, string, string) error
}

type CreateUnitOfWork interface {
	Execute(context.Context, func(CreateTransaction) error) error
}

type Creator struct{ unitOfWork CreateUnitOfWork }

func NewCreator(unitOfWork CreateUnitOfWork) *Creator {
	return &Creator{unitOfWork: unitOfWork}
}

func (c *Creator) Create(ctx context.Context, principalID, userID, idempotencyKey string, input CreateInput) (Created, quota.RecordingQuota, error) {
	if c == nil || c.unitOfWork == nil {
		return Created{}, quota.RecordingQuota{}, errors.New("recording creator is not configured")
	}
	input, err := NormalizeCreateInput(input)
	if err != nil {
		return Created{}, quota.RecordingQuota{}, err
	}
	recordingID, digest := deterministicCreateIdentity(principalID, idempotencyKey, "recording")
	jobID, _ := deterministicCreateIdentity(principalID, idempotencyKey, "recording-job")
	command := CreateCommand{
		PrincipalID: principalID, UserID: userID, RecordingID: recordingID,
		JobID: jobID, RequestDigest: digest, Input: input,
	}

	var created Created
	var currentQuota quota.RecordingQuota
	err = c.unitOfWork.Execute(ctx, func(tx CreateTransaction) error {
		lockedQuota, err := tx.LockQuota(ctx, userID, time.Now().UTC())
		if err != nil {
			return err
		}
		currentQuota = lockedQuota
		existing, found, err := tx.Find(ctx, userID, recordingID)
		if err != nil {
			return err
		}
		if found {
			if !createdMatches(existing, input) {
				return ErrCreateIdempotencyConflict
			}
			created = existing
			return nil
		}
		if violation := ValidateQuota(lockedQuota, input.Duration); violation != nil {
			return violation
		}
		if err := tx.LockMedia(ctx, principalID, input.AudioAssetID, "recording_audio"); err != nil {
			return err
		}
		assetIDs := []string{input.AudioAssetID}
		if input.PhotoAssetID != nil {
			if err := tx.LockMedia(ctx, principalID, *input.PhotoAssetID, "recording_photo"); err != nil {
				return err
			}
			assetIDs = append(assetIDs, *input.PhotoAssetID)
		}
		created, err = tx.Insert(ctx, command)
		if err != nil {
			return err
		}
		if input.InterviewSessionID != nil {
			interviewTx, ok := tx.(InterviewCreateTransaction)
			if !ok {
				return errors.New("interview recording transaction is not configured")
			}
			if err := interviewTx.LinkInterview(ctx, *input.InterviewSessionID, principalID, userID, recordingID); err != nil {
				return err
			}
			created.InterviewSessionID = input.InterviewSessionID
		}
		if err := tx.AttachMedia(ctx, principalID, assetIDs); err != nil {
			return err
		}
		if err := tx.EnqueueProcessing(ctx, command); err != nil {
			return err
		}
		currentQuota = QuotaAfterCreate(lockedQuota, input.Duration)
		return nil
	})
	return created, currentQuota, err
}

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func NormalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.Topic = truncateCreateRunes(strings.TrimSpace(input.Topic), 300)
	if input.Topic == "" {
		return CreateInput{}, &ValidationError{Message: "Recording topic is required"}
	}
	if input.Duration <= 0 {
		return CreateInput{}, &ValidationError{Message: "Recording duration must be positive"}
	}
	input.PracticeType = strings.ToLower(strings.TrimSpace(input.PracticeType))
	switch input.PracticeType {
	case "free_talk", "topic", "photo_description":
	default:
		return CreateInput{}, &ValidationError{Message: "Practice type is invalid"}
	}
	if input.Timestamp.IsZero() {
		return CreateInput{}, &ValidationError{Message: "Recording timestamp is required"}
	}
	input.Timestamp = input.Timestamp.UTC().Truncate(time.Microsecond)
	input.AudioAssetID = strings.TrimSpace(input.AudioAssetID)
	if input.AudioAssetID == "" {
		return CreateInput{}, &ValidationError{Message: "Audio asset is required"}
	}
	if input.PhotoAssetID != nil {
		value := strings.TrimSpace(*input.PhotoAssetID)
		if value == "" {
			input.PhotoAssetID = nil
		} else {
			input.PhotoAssetID = &value
		}
	}
	if input.PracticeType == "photo_description" && input.PhotoAssetID == nil {
		return CreateInput{}, &ValidationError{Message: "Photo asset is required for photo description practice"}
	}
	if input.PhotoObject != nil {
		value := truncateCreateRunes(strings.Join(strings.Fields(*input.PhotoObject), " "), 120)
		if value == "" {
			input.PhotoObject = nil
		} else {
			input.PhotoObject = &value
		}
	}
	if input.PracticeType != "photo_description" {
		input.PhotoObject = nil
	}
	if input.InterviewSessionID != nil {
		value := strings.TrimSpace(*input.InterviewSessionID)
		if value == "" || len(value) > 200 || input.PracticeType != "topic" {
			return CreateInput{}, &ValidationError{Message: "interviewSessionId requires a topic interview"}
		}
		input.InterviewSessionID = &value
	}
	return input, nil
}

func truncateCreateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func deterministicCreateIdentity(principalID string, idempotencyKey string, scope string) (string, string) {
	digestBytes := sha256.Sum256([]byte(scope + "\x1f" + principalID + "\x1f" + idempotencyKey))
	digest := hex.EncodeToString(digestBytes[:])
	var id uuid.UUID
	copy(id[:], digestBytes[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String(), digest
}

type QuotaViolation struct {
	MaxSessionSeconds int
}

func (e *QuotaViolation) Error() string {
	return "recording exceeds account session limit"
}

func ValidateQuota(current quota.RecordingQuota, duration int) *QuotaViolation {
	maximum := current.MaxSessionSeconds
	if maximum <= 0 {
		maximum = quota.AccountMaxSessionSeconds
	}
	if duration > maximum {
		return &QuotaViolation{MaxSessionSeconds: maximum}
	}
	return nil
}

func QuotaAfterCreate(before quota.RecordingQuota, duration int) quota.RecordingQuota {
	after := before
	savedSeconds := NormalizeDurationSeconds(duration)
	after.WeeklyUsedSeconds = NormalizeDurationSeconds(before.WeeklyUsedSeconds) + savedSeconds
	return after
}

func createdMatches(created Created, input CreateInput) bool {
	return created.Topic == input.Topic &&
		created.Duration == input.Duration &&
		created.Timestamp.Equal(input.Timestamp) &&
		created.PracticeType == input.PracticeType &&
		created.AudioAssetID == input.AudioAssetID &&
		equalOptionalString(created.PhotoAssetID, input.PhotoAssetID) &&
		equalOptionalString(created.PhotoObject, input.PhotoObject) &&
		equalOptionalString(created.InterviewSessionID, input.InterviewSessionID)
}

func equalOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
