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

func TestCreatorOrchestratesAtomicRecordingCreation(t *testing.T) {
	remaining := 120
	tx := &createTransactionStub{quota: quota.RecordingQuota{WeeklyRemainingSeconds: &remaining}}
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
	if updatedQuota.WeeklyUsedSeconds != 30 || updatedQuota.WeeklyRemainingSeconds == nil || *updatedQuota.WeeklyRemainingSeconds != 90 {
		t.Fatalf("updated quota = %+v", updatedQuota)
	}
}

func TestCreatorRejectsQuotaBeforeMediaOrPersistence(t *testing.T) {
	remaining := 10
	tx := &createTransactionStub{quota: quota.RecordingQuota{WeeklyRemainingSeconds: &remaining}}
	creator := NewCreator(createUnitOfWorkStub{tx: tx})
	_, _, err := creator.Create(context.Background(), "principal", "user", "request", CreateInput{
		Topic: "Talk", Duration: 30, Timestamp: time.Now().UTC(), PracticeType: "free_talk", AudioAssetID: "audio",
	})
	var violation *QuotaViolation
	if !errors.As(err, &violation) || violation.Remaining != 10 {
		t.Fatalf("err=%v", err)
	}
	if tx.inserted || tx.attached || tx.enqueued || len(tx.lockedPurposes) != 0 {
		t.Fatalf("quota failure reached persistence: %+v", tx)
	}

	tx = &createTransactionStub{quota: quota.RecordingQuota{IsSubscriber: true}}
	creator = NewCreator(createUnitOfWorkStub{tx: tx})
	_, _, err = creator.Create(context.Background(), "principal", "user", "long", CreateInput{
		Topic: "Talk", Duration: quota.SubscriberMaxSessionSeconds + 1,
		Timestamp: time.Now().UTC(), PracticeType: "free_talk", AudioAssetID: "audio",
	})
	if !errors.As(err, &violation) || !violation.SubscriberLimit {
		t.Fatalf("subscriber err=%v", err)
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
