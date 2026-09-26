package httpapi

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/quota"
)

func stringPointer(value string) *string {
	return &value
}

func TestRecordingProcessingTimeoutAllowsMultiPassRetries(t *testing.T) {
	if recordingProcessingTimeout != 30*time.Minute {
		t.Fatalf("processing timeout = %s", recordingProcessingTimeout)
	}
}

func TestRecordingRetryWorkResumesTheStoredFailedStage(t *testing.T) {
	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	tests := []struct {
		name      string
		recording recordingResponse
		wantStage string
		wantAudio string
	}{
		{
			name: "transcription uses saved audio",
			recording: recordingResponse{
				Status: "failed", ProcessingStage: stringPointer("transcribing"),
				AudioDataURL: stringPointer("/uploads/recordings/user-1/recording-1.webm"),
			},
			wantStage: "transcribing",
			wantAudio: filepath.Join(uploadsDir, "recordings", "user-1", "recording-1.webm"),
		},
		{
			name: "transcription accepts private media asset",
			recording: recordingResponse{
				Status: "failed", ProcessingStage: stringPointer("transcribing"),
				Media: &recordingMediaResponse{Audio: &recordingMediaAssetResponse{
					AssetID: "audio-asset", DownloadPath: "/api/v1/media/audio-asset/download",
				}},
			},
			wantStage: "transcribing",
		},
		{
			name: "analysis reuses transcript",
			recording: recordingResponse{
				Status: "failed", ProcessingStage: stringPointer("suggestions"), Transcript: "I go yesterday.",
			},
			wantStage: "suggestions",
		},
		{
			name: "rewrite reuses reviewed suggestions",
			recording: recordingResponse{
				Status: "failed", ProcessingStage: stringPointer("rewriting"), Transcript: "I go yesterday.",
				Suggestions: []suggestion{{Wrong: "go", Right: "went", Explanation: "Use past tense."}},
			},
			wantStage: "rewriting",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			work, err := recordingRetryWorkFor(tc.recording, "b1")
			if err != nil {
				t.Fatal(err)
			}
			if work.Stage != tc.wantStage || work.AudioPath != tc.wantAudio || work.Transcript != tc.recording.Transcript {
				t.Fatalf("work=%#v", work)
			}
		})
	}
}

func TestRecordingRetryWorkRejectsUnavailableInput(t *testing.T) {
	tests := []recordingResponse{
		{Status: "ready", ProcessingStage: stringPointer("suggestions"), Transcript: "I went home."},
		{Status: "failed", ProcessingStage: nil, Transcript: "I went home."},
		{Status: "failed", ProcessingStage: stringPointer("suggestions"), Transcript: "   "},
		{Status: "failed", ProcessingStage: stringPointer("transcribing"), AudioDataURL: nil},
	}
	for _, recording := range tests {
		if _, err := recordingRetryWorkFor(recording, "b1"); err == nil {
			t.Fatalf("expected retry input to be rejected: %#v", recording)
		}
	}
}

func TestRecordingAfterRetryClaimReturnsTheImmediateProcessingState(t *testing.T) {
	startedAt := time.Date(2026, time.September, 21, 12, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		stage               string
		wantTranscript      string
		wantSuggestionCount int
	}{
		{stage: "transcribing", wantTranscript: "", wantSuggestionCount: 0},
		{stage: "suggestions", wantTranscript: "I go yesterday.", wantSuggestionCount: 0},
		{stage: "rewriting", wantTranscript: "I go yesterday.", wantSuggestionCount: 1},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			recording := recordingResponse{
				Status:              "failed",
				ProcessingStage:     stringPointer(tc.stage),
				ProcessingError:     stringPointer("failed"),
				Transcript:          "I go yesterday.",
				CorrectedTranscript: "stale correction",
				Suggestions:         []suggestion{{Wrong: "go", Right: "went"}},
				ShadowingStatus:     "failed",
				ShadowingAudioURL:   stringPointer("/uploads/shadowing/stale.mp3"),
				ShadowingError:      stringPointer("failed"),
			}

			updated := recordingAfterRetryClaim(recording, startedAt)
			if updated.Status != "processing" || updated.ProcessingError != nil {
				t.Fatalf("status was not reset: %#v", updated)
			}
			if updated.Transcript != tc.wantTranscript || len(updated.Suggestions) != tc.wantSuggestionCount {
				t.Fatalf("upstream artifacts were not preserved correctly: %#v", updated)
			}
			if updated.CorrectedTranscript != "" || updated.ShadowingStatus != "pending" || updated.ShadowingAudioURL != nil || updated.ShadowingError != nil {
				t.Fatalf("downstream artifacts were not reset: %#v", updated)
			}
			if updated.ShadowingUpdatedAt != startedAt.Format(time.RFC3339Nano) {
				t.Fatalf("shadowingUpdatedAt=%q", updated.ShadowingUpdatedAt)
			}
		})
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
