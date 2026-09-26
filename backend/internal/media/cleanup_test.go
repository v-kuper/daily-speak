package media

import (
	"context"
	"strings"
	"testing"
)

func TestCleanupRequiresDatabaseAndMediaServiceForSweeps(t *testing.T) {
	cleanup := NewCleanup(nil, nil, nil, nil)
	if cleanup.Available() {
		t.Fatal("incomplete cleanup service reported itself available")
	}
	if err := cleanup.AbortExpiredUploads(context.Background()); err != nil {
		t.Fatalf("disabled cleanup should be a no-op: %v", err)
	}
	if err := cleanup.EnqueueExpiredAssets(context.Background()); err != nil {
		t.Fatalf("disabled cleanup should be a no-op: %v", err)
	}
	if err := cleanup.Delete(context.Background(), "asset-id"); err == nil {
		t.Fatal("delete should reject a missing database")
	}
}

func TestCleanupFailureMessagesAreBounded(t *testing.T) {
	message := strings.Repeat("x", 600)
	if got := truncateCleanupMessage(message, 500); len([]rune(got)) != 500 {
		t.Fatalf("truncated message length = %d", len([]rune(got)))
	}
}
