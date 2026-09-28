package recording

import (
	"testing"

	"daily-speaking-practice/backend/internal/quota"
)

func TestValidateVerifiedDurationReconcilesDeclaredQuotaIdempotently(t *testing.T) {
	remaining := 9
	current := quota.RecordingQuota{WeeklyRemainingSeconds: &remaining}
	if err := validateVerifiedDuration(current, 1, 10); err != nil {
		t.Fatalf("measured duration within remaining quota was rejected: %v", err)
	}
	if err := validateVerifiedDuration(current, 1, 11); err == nil {
		t.Fatal("measured duration beyond remaining quota was accepted")
	}

	remaining = 0
	current.WeeklyRemainingSeconds = &remaining
	if err := validateVerifiedDuration(current, 10, 10); err != nil {
		t.Fatalf("retry after verified duration update was rejected: %v", err)
	}
}

func TestValidateVerifiedDurationEnforcesAccountSessionLimit(t *testing.T) {
	current := quota.RecordingQuota{IsSubscriber: true}
	if err := validateVerifiedDuration(current, 1, quota.SubscriberMaxSessionSeconds+1); err == nil {
		t.Fatal("subscriber recording beyond the session limit was accepted")
	}
}
