package httpapi

import (
	"testing"
)

func TestNormalizeRecordingProcessingStageAcceptsKnownStages(t *testing.T) {
	for _, stage := range []string{"transcribing", "suggestions", "rewriting"} {
		value := stage
		got := normalizeRecordingProcessingStage(&value)
		if got == nil || *got != stage {
			t.Fatalf("expected %q stage, got %#v", stage, got)
		}
	}
}

func TestNormalizeRecordingProcessingStageRejectsUnknownStage(t *testing.T) {
	value := "unexpected"

	if got := normalizeRecordingProcessingStage(&value); got != nil {
		t.Fatalf("expected unknown stage to be rejected, got %#v", got)
	}
}

func TestNormalizeShadowingStatus(t *testing.T) {
	for _, value := range []string{"pending", "processing", "ready", "failed"} {
		if got := normalizeShadowingStatus(value); got != value {
			t.Fatalf("expected %q, got %q", value, got)
		}
	}
	if got := normalizeShadowingStatus("unexpected"); got != "pending" {
		t.Fatalf("expected unknown shadowing status to become pending, got %q", got)
	}
}
