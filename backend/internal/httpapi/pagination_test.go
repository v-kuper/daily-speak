package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParsePageRequest(t *testing.T) {
	timestamp := time.Date(2026, 9, 26, 12, 30, 0, 0, time.UTC)
	cursor, err := encodePageCursor(pageCursor{Timestamp: timestamp, ID: "recording-123"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/recordings?limit=50&cursor="+cursor, nil)
	page, err := parsePageRequest(request)
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	if page.Limit != 50 || page.Cursor == nil || !page.Cursor.Timestamp.Equal(timestamp) || page.Cursor.ID != "recording-123" {
		t.Fatalf("unexpected page request: %+v", page)
	}
}

func TestParsePageRequestRejectsInvalidInput(t *testing.T) {
	for _, target := range []string{
		"/api/v1/recordings?limit=0",
		"/api/v1/recordings?limit=101",
		"/api/v1/recordings?limit=abc",
		"/api/v1/recordings?cursor=not-a-cursor",
	} {
		request := httptest.NewRequest("GET", target, nil)
		if _, err := parsePageRequest(request); err == nil {
			t.Fatalf("expected %s to be rejected", target)
		}
	}
}
