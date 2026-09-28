package quota

import "testing"

func TestQuotaFormattingAndBounds(t *testing.T) {
	if got := nonNegative(-1); got != 0 {
		t.Fatalf("non-negative = %d", got)
	}
	if got := FormatSeconds(65); got != "1:05" {
		t.Fatalf("formatted seconds = %q", got)
	}
}

func TestRecordingQuotaKeepsLegacyFreeFieldsPositiveWithoutEnforcingWeeklyUsage(t *testing.T) {
	free := recordingQuota(false, 3600)
	if free.WeeklyLimitSeconds == nil || *free.WeeklyLimitSeconds != legacyWeeklyCompatibilitySeconds ||
		free.WeeklyRemainingSeconds == nil || *free.WeeklyRemainingSeconds != legacyWeeklyCompatibilitySeconds {
		t.Fatalf("free compatibility fields = %+v", free)
	}
	if free.WeeklyUsedSeconds != 3600 || free.MaxSessionSeconds != AccountMaxSessionSeconds {
		t.Fatalf("free recording policy = %+v", free)
	}

	subscriber := recordingQuota(true, 3600)
	if subscriber.WeeklyLimitSeconds != nil || subscriber.WeeklyRemainingSeconds != nil ||
		subscriber.WeeklyUsedSeconds != 3600 || subscriber.MaxSessionSeconds != AccountMaxSessionSeconds {
		t.Fatalf("subscriber recording policy = %+v", subscriber)
	}
}
