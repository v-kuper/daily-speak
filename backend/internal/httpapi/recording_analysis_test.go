package httpapi

import (
	"context"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/quota"
)

func TestRecordingProcessingTimeoutAllowsMultiPassRetries(t *testing.T) {
	if recordingProcessingTimeout != 30*time.Minute {
		t.Fatalf("processing timeout = %s", recordingProcessingTimeout)
	}
}

type stubChatClient struct {
	post func(context.Context, any) (ai.ChatResponse, error)
}

func (s stubChatClient) PostChat(ctx context.Context, body any) (ai.ChatResponse, error) {
	return s.post(ctx, body)
}

func TestNewServerUsesInjectedAIClient(t *testing.T) {
	client := stubChatClient{post: func(context.Context, any) (ai.ChatResponse, error) {
		return ai.ChatResponse{}, nil
	}}
	server := NewServer(Config{AIClient: client})
	if server.aiClient == nil {
		t.Fatal("expected injected AI client")
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
