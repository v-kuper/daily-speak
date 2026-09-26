package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
)

func TestGuestPreviewRequestValidationAndIdempotencyDigest(t *testing.T) {
	first, firstTime, err := normalizeGuestPreviewCreate(guestPreviewCreateRequest{
		AudioAssetID: " asset-1 ", Topic: " My day ", Duration: 30, PracticeType: " FREE_TALK ",
	}, time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("normalize first request: %v", err)
	}
	second, secondTime, err := normalizeGuestPreviewCreate(guestPreviewCreateRequest{
		AudioAssetID: "asset-1", Topic: "My day", Duration: 30, PracticeType: "free_talk",
	}, time.Date(2026, 9, 26, 10, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("normalize retry: %v", err)
	}
	if first.Timestamp != "" || second.Timestamp != "" || firstTime == secondTime {
		t.Fatalf("server timestamps should differ without changing request identity: %#v %#v", first, second)
	}
	if guestPreviewRequestDigest(first) != guestPreviewRequestDigest(second) {
		t.Fatal("an idempotent retry without an explicit timestamp must keep the same digest")
	}
	for name, input := range map[string]guestPreviewCreateRequest{
		"too long": {AudioAssetID: "asset", Topic: "Topic", Duration: 61, PracticeType: "free_talk"},
		"photo":    {AudioAssetID: "asset", Topic: "Topic", Duration: 10, PracticeType: "photo_description"},
		"no asset": {Topic: "Topic", Duration: 10, PracticeType: "topic"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := normalizeGuestPreviewCreate(input, time.Now()); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestParseGuestPreviewCorrectionsKeepsOnlyTwoHighConfidenceMaterialErrors(t *testing.T) {
	transcript := "Yesterday I go to work and she have a meeting. It was nice."
	input := `{"corrections":[
		{"wrong":"Yesterday I go","right":"Yesterday I went","explanation":"Use past tense.","category":"verb_grammar","severity":"major","confidence":0.99},
		{"wrong":"she have","right":"she has","explanation":"Match the subject.","category":"verb_grammar","severity":"medium","confidence":0.95},
		{"wrong":"It was nice","right":"It was pleasant","explanation":"A style alternative.","category":"naturalness","severity":"minor","confidence":0.99},
		{"wrong":"work","right":"the office","explanation":"Uncertain preference.","category":"vocabulary","severity":"major","confidence":0.70}
	]}`
	got := parseGuestPreviewCorrections(input, transcript)
	if len(got) != 2 || got[0].Wrong != "Yesterday I go" || got[1].Wrong != "she have" {
		t.Fatalf("unexpected preview corrections: %#v", got)
	}
}

func TestGenerateGuestPreviewCorrectionsUsesOneAIRequest(t *testing.T) {
	client := &countingGuestPreviewAI{response: `{"corrections":[]}`}
	server := NewServer(Config{AIClient: client})
	got, err := server.generateGuestPreviewCorrections(context.Background(), "I went home.")
	if err != nil {
		t.Fatalf("generate preview corrections: %v", err)
	}
	if client.calls != 1 || len(got) != 0 {
		t.Fatalf("calls=%d corrections=%#v", client.calls, got)
	}
	if !strings.Contains(client.body, "at most two") {
		t.Fatalf("preview prompt did not contain the bounded contract: %s", client.body)
	}
}

func TestWorkerConfigReadsGuestPreviewConcurrency(t *testing.T) {
	t.Setenv("WORKER_GUEST_PREVIEW_CONCURRENCY", "3")
	config, err := WorkerConfigFromEnv()
	if err != nil {
		t.Fatalf("worker config: %v", err)
	}
	if config.GuestPreviewConcurrency != 3 {
		t.Fatalf("guest preview concurrency = %d", config.GuestPreviewConcurrency)
	}
}

func TestGuestPreviewQueueCapacityUsesSafeBounds(t *testing.T) {
	t.Setenv("GUEST_PREVIEW_QUEUE_CAPACITY", "37")
	if got := guestPreviewQueueCapacity(); got != 37 {
		t.Fatalf("queue capacity = %d", got)
	}
	t.Setenv("GUEST_PREVIEW_QUEUE_CAPACITY", "0")
	if got := guestPreviewQueueCapacity(); got != defaultGuestPreviewQueueCap {
		t.Fatalf("invalid queue capacity = %d", got)
	}
}

type countingGuestPreviewAI struct {
	calls    int
	response string
	body     string
}

func (client *countingGuestPreviewAI) PostChat(_ context.Context, body any) (ai.ChatResponse, error) {
	client.calls++
	client.body = strings.ReplaceAll(strings.TrimSpace(toJSON(body)), "\\u003c", "<")
	return ai.ChatResponse{Response: client.response}, nil
}

func toJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
