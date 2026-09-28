package guestpreview

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeCreateAndDigestAreStableAcrossServerTimestamps(t *testing.T) {
	first, firstTime, err := NormalizeCreate(CreateRequest{AudioAssetID: " asset-1 ", Topic: " My day ", Duration: 30, PracticeType: " FREE_TALK "}, time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, secondTime, err := NormalizeCreate(CreateRequest{AudioAssetID: "asset-1", Topic: "My day", Duration: 30, PracticeType: "free_talk"}, time.Date(2026, 9, 26, 10, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.Timestamp != "" || second.Timestamp != "" || firstTime == secondTime {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if RequestDigest(first) != RequestDigest(second) {
		t.Fatal("digest changed across server timestamps")
	}
}

func TestNormalizeCreateUsesTheSameTopicLimitAsInterviews(t *testing.T) {
	request := CreateRequest{AudioAssetID: "asset", Topic: strings.Repeat("a", 300), Duration: 10, PracticeType: "topic"}
	if _, _, err := NormalizeCreate(request, time.Now()); err != nil {
		t.Fatalf("300-rune topic was rejected: %v", err)
	}
	request.Topic += "b"
	if _, _, err := NormalizeCreate(request, time.Now()); err == nil {
		t.Fatal("301-rune topic was accepted")
	}
}

func TestNormalizeCreateRejectsUnsupportedRequests(t *testing.T) {
	for name, input := range map[string]CreateRequest{
		"too long": {AudioAssetID: "asset", Topic: "Topic", Duration: 181, PracticeType: "free_talk"},
		"photo":    {AudioAssetID: "asset", Topic: "Topic", Duration: 10, PracticeType: "photo_description"},
		"no asset": {Topic: "Topic", Duration: 10, PracticeType: "topic"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NormalizeCreate(input, time.Now()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestQueueCapacityFromEnvUsesSafeBounds(t *testing.T) {
	t.Setenv("GUEST_PREVIEW_QUEUE_CAPACITY", "37")
	if got := QueueCapacityFromEnv(); got != 37 {
		t.Fatalf("got=%d", got)
	}
	t.Setenv("GUEST_PREVIEW_QUEUE_CAPACITY", "0")
	if got := QueueCapacityFromEnv(); got != DefaultQueueCapacity {
		t.Fatalf("got=%d", got)
	}
}
