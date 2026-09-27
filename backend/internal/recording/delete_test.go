package recording

import (
	"context"
	"errors"
	"testing"

	"daily-speaking-practice/backend/internal/quota"
)

type deletionUnitOfWorkStub struct{ tx *deletionTransactionStub }

func (u deletionUnitOfWorkStub) Execute(ctx context.Context, operation func(DeletionTransaction) error) error {
	return operation(u.tx)
}

type deletionTransactionStub struct {
	source       DeletionSource
	found        bool
	legacyQueued []string
	assetsQueued []string
	removed      bool
}

func (s *deletionTransactionStub) Load(context.Context, string, string) (DeletionSource, bool, error) {
	return s.source, s.found, nil
}
func (s *deletionTransactionStub) QueueLegacyMedia(_ context.Context, value string, _ string) error {
	s.legacyQueued = append(s.legacyQueued, value)
	return nil
}
func (s *deletionTransactionStub) QueueAsset(_ context.Context, value string, _ string) error {
	s.assetsQueued = append(s.assetsQueued, value)
	return nil
}
func (s *deletionTransactionStub) Remove(context.Context, string, string) (bool, error) {
	s.removed = true
	return true, nil
}

type legacyValidatorStub struct{}

func (legacyValidatorStub) Path(value string) (string, error) {
	if value == "/uploads/recordings/user/audio.webm" {
		return "/tmp/audio.webm", nil
	}
	return "", errors.New("outside legacy storage")
}

type deletionQuotaStub struct{ value quota.RecordingQuota }

func (s deletionQuotaStub) Get(context.Context, string, bool) (quota.RecordingQuota, error) {
	return s.value, nil
}

func TestDeleterBuildsBoundedDeduplicatedCleanupPlan(t *testing.T) {
	legacyURL := "/uploads/recordings/user/audio.webm"
	unsafeURL := "https://external.invalid/audio.webm"
	assetID := "asset-1"
	tx := &deletionTransactionStub{
		found: true,
		source: DeletionSource{
			LegacyURLs: []*string{&legacyURL, &legacyURL, &unsafeURL},
			AssetIDs:   []*string{&assetID, &assetID},
		},
	}
	ids := []string{"legacy-job", "asset-job"}
	index := 0
	deleter := NewDeleter(
		deletionUnitOfWorkStub{tx: tx}, legacyValidatorStub{},
		deletionQuotaStub{value: quota.RecordingQuota{WeeklyUsedSeconds: 30}},
		func() string { value := ids[index]; index++; return value },
	)
	result, err := deleter.Delete(context.Background(), "user", false, "recording")
	if err != nil {
		t.Fatal(err)
	}
	if !tx.removed || len(tx.legacyQueued) != 1 || tx.legacyQueued[0] != legacyURL {
		t.Fatalf("legacy cleanup = %v, removed=%t", tx.legacyQueued, tx.removed)
	}
	if len(tx.assetsQueued) != 1 || tx.assetsQueued[0] != assetID {
		t.Fatalf("asset cleanup = %v", tx.assetsQueued)
	}
	if result.RecordingID != "recording" || result.Quota == nil || result.Quota.WeeklyUsedSeconds != 30 {
		t.Fatalf("result=%+v", result)
	}
}

func TestDeleterStopsWhenRecordingIsNotOwned(t *testing.T) {
	tx := &deletionTransactionStub{}
	deleter := NewDeleter(deletionUnitOfWorkStub{tx: tx}, legacyValidatorStub{}, nil, func() string { return "job" })
	_, err := deleter.Delete(context.Background(), "user", false, "missing")
	if !errors.Is(err, ErrDeleteNotFound) {
		t.Fatalf("err=%v", err)
	}
	if tx.removed || len(tx.legacyQueued) != 0 || len(tx.assetsQueued) != 0 {
		t.Fatalf("unexpected mutation: %+v", tx)
	}
}
