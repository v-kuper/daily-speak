package recording

import (
	"context"
	"errors"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/quota"
)

type createUnitOfWorkStub struct{ tx *createTransactionStub }

func (u createUnitOfWorkStub) Execute(ctx context.Context, operation func(CreateTransaction) error) error {
	return operation(u.tx)
}

type createTransactionStub struct {
	quota          quota.RecordingQuota
	existing       Created
	found          bool
	lockedPurposes []string
	inserted       bool
	attached       bool
	enqueued       bool
	linked         bool
}

func (s *createTransactionStub) LockQuota(context.Context, string, time.Time) (quota.RecordingQuota, error) {
	return s.quota, nil
}
func (s *createTransactionStub) Find(context.Context, string, string) (Created, bool, error) {
	return s.existing, s.found, nil
}
func (s *createTransactionStub) LockMedia(_ context.Context, _, _, purpose string) error {
	s.lockedPurposes = append(s.lockedPurposes, purpose)
	return nil
}
func (s *createTransactionStub) Insert(_ context.Context, command CreateCommand) (Created, error) {
	s.inserted = true
	return Created{
		ID: command.RecordingID, Topic: command.Input.Topic, Duration: command.Input.Duration,
		Timestamp: command.Input.Timestamp, PracticeType: command.Input.PracticeType,
		AudioAssetID: command.Input.AudioAssetID, PhotoAssetID: command.Input.PhotoAssetID,
	}, nil
}
func (s *createTransactionStub) AttachMedia(context.Context, string, []string) error {
	s.attached = true
	return nil
}
func (s *createTransactionStub) EnqueueProcessing(context.Context, CreateCommand) error {
	s.enqueued = true
	return nil
}
func (s *createTransactionStub) LinkInterview(_ context.Context, sessionID, principalID, userID, recordingID string) error {
	if sessionID == "" || principalID == "" || userID == "" || recordingID == "" || s.enqueued {
		return ErrInterviewSessionUnavailable
	}
	s.linked = true
	return nil
}

func TestCreatorOrchestratesAtomicRecordingCreation(t *testing.T) {
	compatibility := quota.AccountMaxSessionSeconds
	tx := &createTransactionStub{quota: quota.RecordingQuota{
		WeeklyLimitSeconds: &compatibility, WeeklyRemainingSeconds: &compatibility,
		MaxSessionSeconds: quota.AccountMaxSessionSeconds,
	}}
	creator := NewCreator(createUnitOfWorkStub{tx: tx})
	photoID := "photo"
	input := CreateInput{
		Topic: "Park", Duration: 30, Timestamp: time.Now().UTC().Truncate(time.Microsecond),
		PracticeType: "photo_description", AudioAssetID: "audio", PhotoAssetID: &photoID,
	}
	created, updatedQuota, err := creator.Create(context.Background(), "principal", "user", "request", input)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || !tx.inserted || !tx.attached || !tx.enqueued {
		t.Fatalf("created=%+v tx=%+v", created, tx)
	}
	if len(tx.lockedPurposes) != 2 || tx.lockedPurposes[0] != "recording_audio" || tx.lockedPurposes[1] != "recording_photo" {
		t.Fatalf("locked purposes = %v", tx.lockedPurposes)
	}
	if updatedQuota.WeeklyUsedSeconds != 30 || updatedQuota.WeeklyLimitSeconds == nil || *updatedQuota.WeeklyLimitSeconds != compatibility ||
		updatedQuota.WeeklyRemainingSeconds == nil || *updatedQuota.WeeklyRemainingSeconds != compatibility {
		t.Fatalf("updated quota = %+v", updatedQuota)
	}
}

func TestCreatorRejectsPerRecordingLimitBeforeMediaOrPersistence(t *testing.T) {
	tx := &createTransactionStub{quota: quota.RecordingQuota{MaxSessionSeconds: quota.AccountMaxSessionSeconds}}
	creator := NewCreator(createUnitOfWorkStub{tx: tx})
	_, _, err := creator.Create(context.Background(), "principal", "user", "request", CreateInput{
		Topic: "Talk", Duration: quota.AccountMaxSessionSeconds + 1,
		Timestamp: time.Now().UTC(), PracticeType: "free_talk", AudioAssetID: "audio",
	})
	var violation *QuotaViolation
	if !errors.As(err, &violation) || violation.MaxSessionSeconds != quota.AccountMaxSessionSeconds {
		t.Fatalf("err=%v", err)
	}
	if tx.inserted || tx.attached || tx.enqueued || len(tx.lockedPurposes) != 0 {
		t.Fatalf("quota failure reached persistence: %+v", tx)
	}
}

func TestValidateQuotaIgnoresWeeklyUsageAndSubscription(t *testing.T) {
	zero := 0
	for _, current := range []quota.RecordingQuota{
		{IsSubscriber: false, WeeklyRemainingSeconds: &zero, MaxSessionSeconds: quota.AccountMaxSessionSeconds},
		{IsSubscriber: true, MaxSessionSeconds: quota.AccountMaxSessionSeconds},
	} {
		if violation := ValidateQuota(current, quota.AccountMaxSessionSeconds); violation != nil {
			t.Fatalf("account recording at limit rejected for quota %+v: %v", current, violation)
		}
	}
}

func TestCreatorLinksInterviewBeforeEnqueueAndRejectsChangedRetry(t *testing.T) {
	tx := &createTransactionStub{quota: quota.RecordingQuota{MaxSessionSeconds: quota.AccountMaxSessionSeconds}}
	creator := NewCreator(createUnitOfWorkStub{tx: tx})
	sessionID := "session-1"
	input := CreateInput{Topic: "Travel", Duration: 30, Timestamp: time.Now().UTC().Truncate(time.Microsecond),
		PracticeType: "topic", AudioAssetID: "audio", InterviewSessionID: &sessionID}
	created, _, err := creator.Create(context.Background(), "principal", "user", "request", input)
	if err != nil || !tx.linked || !tx.enqueued || created.InterviewSessionID == nil {
		t.Fatalf("interview was not linked before enqueue: created=%+v tx=%+v err=%v", created, tx, err)
	}
	tx.found, tx.existing = true, created
	otherSession := "session-2"
	input.InterviewSessionID = &otherSession
	_, _, err = creator.Create(context.Background(), "principal", "user", "request", input)
	if !errors.Is(err, ErrCreateIdempotencyConflict) {
		t.Fatalf("changed interview session must conflict on retry: %v", err)
	}
}

func TestNormalizeCreateInputOwnsRecordingRules(t *testing.T) {
	_, err := NormalizeCreateInput(CreateInput{
		Topic: "Describe it", Duration: 30, Timestamp: time.Now().UTC(),
		PracticeType: "photo_description", AudioAssetID: "audio-1",
	})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Message != "Photo asset is required for photo description practice" {
		t.Fatalf("err=%v", err)
	}
}

func TestDeterministicCreateIdentity(t *testing.T) {
	firstID, firstDigest := deterministicCreateIdentity("principal-1", "retry-key", "recording")
	secondID, secondDigest := deterministicCreateIdentity("principal-1", "retry-key", "recording")
	otherID, _ := deterministicCreateIdentity("principal-1", "other-key", "recording")
	if firstID != secondID || firstDigest != secondDigest {
		t.Fatal("same principal and idempotency key must produce the same identity")
	}
	if firstID == otherID || len(firstDigest) != 64 {
		t.Fatalf("unexpected deterministic identity %q / %q", firstID, firstDigest)
	}
}
