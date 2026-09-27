package recordingsession

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/recording"
)

var (
	ErrNotFound     = errors.New("recording upload session not found")
	ErrFinalized    = errors.New("recording upload session is already finalized")
	ErrFormatChange = errors.New("recording chunk format changed during upload")
	ErrAudioPending = errors.New("recording audio is still uploading")
)

type ValidationError struct{ Message string }

func (err *ValidationError) Error() string { return err.Message }

type Files interface {
	SaveChunk(sessionID string, index int, extension string, data []byte) error
	SaveFinal(sessionID string, extension string, data []byte) error
	FinalExists(sessionID string, extension string) (bool, error)
	Publish(sessionID string, userID string, recordingID string, extension string, expectedChunks int) (string, error)
	DiscardPublished(publicURL string) error
	Remove(sessionID string) error
}

type Repository interface {
	Create(context.Context, StartCommand) error
	Load(context.Context, string, string) (Session, bool, error)
	UpdateChunk(context.Context, string, string, string, int) error
	UpdateFinal(context.Context, string, string, string) error
	GetQuota(context.Context, string, bool) (quota.RecordingQuota, error)
	LoadRecording(context.Context, string, string) (Recording, bool, error)
	ExecuteFinalize(context.Context, func(FinalizeTransaction) error) error
}

type FinalizeTransaction interface {
	LockSession(context.Context, string, string) (Session, bool, error)
	LockQuota(context.Context, string, time.Time) (quota.RecordingQuota, error)
	InsertRecording(context.Context, FinalizeCommand) (Recording, error)
	MarkFinalized(context.Context, string, string, string) (bool, error)
	EnqueueProcessing(context.Context, FinalizeCommand) error
}

type FinalizeResult struct {
	Recording Recording
	Quota     *quota.RecordingQuota
	Created   bool
}

type Service struct {
	repository Repository
	files      Files
	newID      func() string
	now        func() time.Time
}

func NewService(repository Repository, files Files, newID func() string) *Service {
	return &Service{repository: repository, files: files, newID: newID, now: time.Now}
}

func (service *Service) Start(ctx context.Context, userID string, input StartInput) (string, error) {
	if err := service.configured(); err != nil {
		return "", err
	}
	command, err := normalizeStartInput(service.newID(), userID, input)
	if err != nil {
		return "", err
	}
	if err := service.repository.Create(ctx, command); err != nil {
		return "", err
	}
	return command.ID, nil
}

func (service *Service) SaveChunk(ctx context.Context, userID string, sessionID string, index int, extension string, data []byte) error {
	if err := service.configured(); err != nil {
		return err
	}
	session, err := service.openSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if session.AudioExtension != nil && *session.AudioExtension != extension {
		return ErrFormatChange
	}
	if err := service.files.SaveChunk(session.ID, index, extension, data); err != nil {
		return err
	}
	return service.repository.UpdateChunk(ctx, session.ID, userID, extension, index+1)
}

func (service *Service) SaveFinal(ctx context.Context, userID string, sessionID string, extension string, data []byte) error {
	if err := service.configured(); err != nil {
		return err
	}
	session, err := service.openSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if err := service.files.SaveFinal(session.ID, extension, data); err != nil {
		return err
	}
	return service.repository.UpdateFinal(ctx, session.ID, userID, extension)
}

func (service *Service) Finalize(ctx context.Context, userID string, subscriber bool, sessionID string, input FinalizeInput) (FinalizeResult, error) {
	if err := service.configured(); err != nil {
		return FinalizeResult{}, err
	}
	session, found, err := service.repository.Load(ctx, userID, strings.TrimSpace(sessionID))
	if err != nil {
		return FinalizeResult{}, err
	}
	if !found {
		return FinalizeResult{}, ErrNotFound
	}
	if session.Status != "open" {
		return service.existingFinalization(ctx, userID, session)
	}
	if session.AudioExtension == nil {
		return FinalizeResult{}, ErrAudioPending
	}
	hasFinal, err := service.files.FinalExists(session.ID, *session.AudioExtension)
	if err != nil {
		return FinalizeResult{}, err
	}
	if !hasFinal {
		return FinalizeResult{}, ErrAudioPending
	}

	duration := session.Duration
	if input.Duration > 0 {
		duration = domain.ToNonNegativeInt(input.Duration)
	}
	timestamp := session.Timestamp
	if input.Timestamp != nil {
		timestamp = input.Timestamp.UTC()
	}
	currentQuota, err := service.repository.GetQuota(ctx, userID, subscriber)
	if err != nil {
		return FinalizeResult{}, err
	}
	if violation := recording.ValidateQuota(currentQuota, duration); violation != nil {
		return FinalizeResult{}, violation
	}

	command := FinalizeCommand{
		Session: session, RecordingID: service.newID(), JobID: service.newID(),
		Duration: duration, Timestamp: timestamp,
	}
	command.AudioURL, err = service.files.Publish(
		session.ID, userID, command.RecordingID, *session.AudioExtension, session.ChunkCount,
	)
	if err != nil {
		return FinalizeResult{}, err
	}
	keepPublished := false
	defer func() {
		if !keepPublished {
			_ = service.files.DiscardPublished(command.AudioURL)
		}
	}()

	var created Recording
	var finalizedByAnother *Session
	err = service.repository.ExecuteFinalize(ctx, func(tx FinalizeTransaction) error {
		locked, found, err := tx.LockSession(ctx, session.ID, userID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if locked.Status != "open" {
			finalizedByAnother = &locked
			return ErrFinalized
		}
		lockedQuota, err := tx.LockQuota(ctx, userID, service.now().UTC())
		if err != nil {
			return err
		}
		if violation := recording.ValidateQuota(lockedQuota, duration); violation != nil {
			return violation
		}
		created, err = tx.InsertRecording(ctx, command)
		if err != nil {
			return err
		}
		updated, err := tx.MarkFinalized(ctx, session.ID, userID, command.RecordingID)
		if err != nil {
			return err
		}
		if !updated {
			return ErrFinalized
		}
		if err := tx.EnqueueProcessing(ctx, command); err != nil {
			return err
		}
		currentQuota = recording.QuotaAfterCreate(lockedQuota, duration)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrFinalized) && finalizedByAnother != nil {
			return service.existingFinalization(ctx, userID, *finalizedByAnother)
		}
		return FinalizeResult{}, err
	}
	keepPublished = true
	_ = service.files.Remove(session.ID)
	if refreshed, refreshErr := service.repository.GetQuota(ctx, userID, subscriber); refreshErr == nil {
		currentQuota = refreshed
	}
	return FinalizeResult{Recording: created, Quota: &currentQuota, Created: true}, nil
}

func (service *Service) openSession(ctx context.Context, userID string, sessionID string) (Session, error) {
	session, found, err := service.repository.Load(ctx, userID, strings.TrimSpace(sessionID))
	if err != nil {
		return Session{}, err
	}
	if !found {
		return Session{}, ErrNotFound
	}
	if session.Status != "open" {
		return Session{}, ErrFinalized
	}
	return session, nil
}

func (service *Service) existingFinalization(ctx context.Context, userID string, session Session) (FinalizeResult, error) {
	if session.RecordingID == nil {
		return FinalizeResult{}, ErrFinalized
	}
	existing, found, err := service.repository.LoadRecording(ctx, userID, *session.RecordingID)
	if err != nil {
		return FinalizeResult{}, err
	}
	if !found {
		return FinalizeResult{}, ErrFinalized
	}
	return FinalizeResult{Recording: existing, Created: false}, nil
}

func (service *Service) configured() error {
	if service == nil || service.repository == nil || service.files == nil || service.newID == nil || service.now == nil {
		return errors.New("recording session service is not configured")
	}
	return nil
}

func normalizeStartInput(id string, userID string, input StartInput) (StartCommand, error) {
	practiceType := domain.NormalizePracticeType(input.PracticeType)
	topic := strings.TrimSpace(input.Topic)
	photoDataURL := domain.NormalizePhotoDataURL(input.PhotoDataURL)
	photoObject := domain.NormalizePhotoObject(input.PhotoObject)
	if practiceType == "photo_description" {
		if photoDataURL == nil {
			return StartCommand{}, &ValidationError{Message: "Photo is required for photo description practice."}
		}
		if topic == "" {
			topic = "Photo description"
		}
	}
	if topic == "" {
		return StartCommand{}, &ValidationError{Message: "Recording topic is required."}
	}
	if input.Timestamp.IsZero() {
		input.Timestamp = time.Now().UTC()
	}
	command := StartCommand{
		ID: id, UserID: userID, Topic: truncateRunes(topic, 300),
		Duration: domain.ToNonNegativeInt(input.Duration), Timestamp: input.Timestamp.UTC(),
		PracticeType: practiceType,
	}
	if practiceType == "photo_description" {
		command.PhotoDataURL = photoDataURL
		command.PhotoObject = photoObject
	}
	return command, nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
