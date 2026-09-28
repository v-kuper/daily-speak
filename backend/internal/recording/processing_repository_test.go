package recording

import (
	"testing"

	"daily-speaking-practice/backend/internal/quota"
)

func TestValidateVerifiedDurationAcceptsAccountRecordingAtLimit(t *testing.T) {
	if err := validateVerifiedDuration(quota.AccountMaxSessionSeconds); err != nil {
		t.Fatalf("measured duration at account limit was rejected: %v", err)
	}
}

func TestValidateVerifiedDurationEnforcesAccountSessionLimit(t *testing.T) {
	if err := validateVerifiedDuration(quota.AccountMaxSessionSeconds + 1); err == nil {
		t.Fatal("account recording beyond the session limit was accepted")
	}
}
