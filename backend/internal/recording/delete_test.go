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
	assetsQueued []string
	removed      bool
}

func (s *deletionTransactionStub) Load(context.Context, string, string) (DeletionSource, bool, error) {
	return s.source, s.found, nil
}
func (s *deletionTransactionStub) QueueAsset(_ context.Context, value string, _ string) error {
	s.assetsQueued = append(s.assetsQueued, value)
	return nil
}
func (s *deletionTransactionStub) Remove(context.Context, string, string) (bool, error) {
	s.removed = true
	return true, nil
}

type deletionQuotaStub struct{ value quota.RecordingQuota }

func (s deletionQuotaStub) Get(context.Context, string, bool) (quota.RecordingQuota, error) {
	return s.value, nil
}

func TestDeleterBuildsDeduplicatedAssetCleanupPlan(t *testing.T) {
	assetID := "asset-1"
	tx := &deletionTransactionStub{
		found: true,
		source: DeletionSource{
			AssetIDs: []*string{&assetID, &assetID},
		},
	}
	deleter := NewDeleter(
		deletionUnitOfWorkStub{tx: tx},
		deletionQuotaStub{value: quota.RecordingQuota{WeeklyUsedSeconds: 30}},
		func() string { return "asset-job" },
	)
	result, err := deleter.Delete(context.Background(), "user", false, "recording")
	if err != nil {
		t.Fatal(err)
	}
	if !tx.removed {
		t.Fatal("recording was not removed")
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
	deleter := NewDeleter(deletionUnitOfWorkStub{tx: tx}, nil, func() string { return "job" })
	_, err := deleter.Delete(context.Background(), "user", false, "missing")
	if !errors.Is(err, ErrDeleteNotFound) {
		t.Fatalf("err=%v", err)
	}
	if tx.removed || len(tx.assetsQueued) != 0 {
		t.Fatalf("unexpected mutation: %+v", tx)
	}
}
