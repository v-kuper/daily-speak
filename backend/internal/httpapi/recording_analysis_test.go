package httpapi

import (
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/quota"
)

func TestParseNaturalTranscriptFromContentReadsCorrectedTranscript(t *testing.T) {
	got := parseNaturalTranscriptFromContent(`{"correctedTranscript":"I went to the store yesterday."}`)

	if got != "I went to the store yesterday." {
		t.Fatalf("expected corrected transcript, got %q", got)
	}
}

func TestParseNaturalTranscriptFromContentRejectsEmptyText(t *testing.T) {
	got := parseNaturalTranscriptFromContent(`{"correctedTranscript":"   "}`)

	if got != "" {
		t.Fatalf("expected empty corrected transcript to be rejected, got %q", got)
	}
}

func TestParseSuggestionsResultFromContentAcceptsValidEmptyList(t *testing.T) {
	got, valid := parseSuggestionsResultFromContent(`{"suggestions":[]}`)

	if !valid || len(got) != 0 {
		t.Fatalf("expected a valid empty suggestion list, got %#v, valid=%v", got, valid)
	}
}

func TestParseSuggestionsResultFromContentRejectsMalformedSuggestions(t *testing.T) {
	_, valid := parseSuggestionsResultFromContent(`{"suggestions":[{"wrong":"I go"}]}`)

	if valid {
		t.Fatal("expected an incomplete suggestion to be rejected")
	}
}

func TestRecordingNaturalVersionPromptUsesLearnerLevelAndExistingCorrections(t *testing.T) {
	prompt := recordingNaturalVersionPrompt(
		"Yesterday I go to the store.",
		[]suggestion{{Wrong: "I go", Right: "I went", Explanation: "Use past tense."}},
		"b1",
	)

	for _, fragment := range []string{
		"Learner level: B1.",
		"Use clear intermediate vocabulary",
		"preserve the speaker's meaning",
		`"wrong":"I go"`,
		`"correctedTranscript"`,
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected prompt to contain %q, got %q", fragment, prompt)
		}
	}
}

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

func TestRecordingQuotaAfterSaveUpdatesFreeUsage(t *testing.T) {
	limit := 600
	remaining := 480
	before := quota.RecordingQuota{
		WeeklyLimitSeconds:     &limit,
		WeeklyUsedSeconds:      120,
		WeeklyRemainingSeconds: &remaining,
		MaxSessionSeconds:      600,
	}

	after := recordingQuotaAfterSave(before, 45)

	if after.WeeklyUsedSeconds != 165 || after.WeeklyRemainingSeconds == nil || *after.WeeklyRemainingSeconds != 435 {
		t.Fatalf("unexpected quota after save: %#v", after)
	}
	if *before.WeeklyRemainingSeconds != 480 {
		t.Fatalf("input quota was mutated: %#v", before)
	}
}
