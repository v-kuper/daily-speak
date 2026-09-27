package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMediaUploadCollectionRouteDoesNotRedirect(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", strings.NewReader(`{}`))

	newTestServer(Config{}).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("media upload collection status = %d, body=%s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("media upload collection redirected to %q", location)
	}
}

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
