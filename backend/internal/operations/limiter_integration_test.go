package operations

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestLimiterSharesAtomicDecisionsThroughPostgreSQL(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	admin, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "rate_limit_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := db.Connect(ctx, parsed.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 12, 0, 5, 0, time.UTC)
	first := NewLimiter(database)
	second := NewLimiter(database)
	first.now = func() time.Time { return now }
	second.now = func() time.Time { return now }
	limit := Limit{Requests: 2, Window: time.Minute}
	for index, limiter := range []*Limiter{first, second, first} {
		decision, err := limiter.Allow(ctx, "auth", "ip:198.51.100.8", limit)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed != (index < 2) {
			t.Fatalf("decision %d allowed=%t", index+1, decision.Allowed)
		}
	}
	first.now = func() time.Time { return now.Add(time.Minute) }
	decision, err := first.Allow(ctx, "auth", "ip:198.51.100.8", limit)
	if err != nil || !decision.Allowed || decision.Remaining != 1 {
		t.Fatalf("new window decision=%+v err=%v", decision, err)
	}
	if _, err := database.Exec(ctx, `UPDATE api_rate_limits SET expires_at = NOW() - INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	removed, err := PruneExpiredRateLimits(ctx, database, now.Add(25*time.Hour), 100)
	if err != nil || removed != 1 {
		t.Fatalf("pruned=%d err=%v", removed, err)
	}
}
