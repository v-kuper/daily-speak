package workqueue

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestQueueClaimsOnceRecoversExpiredLeaseAndFencesOldOwner(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	admin, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(admin.Close)
	schema := "workqueue_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	database, err := db.Connect(ctx, parsedURL.String(), false)
	if err != nil {
		t.Fatalf("connect isolated test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database schema %s: %v", schema, err)
	}

	jobID := uuid.NewString()
	idempotencyKey := "queue-test:" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM processing_jobs WHERE idempotency_key = $1`, idempotencyKey)
	})
	store := NewStore(database)
	job := NewJob{
		ID:             jobID,
		Kind:           KindMediaDelete,
		ResourceID:     "/uploads/test/" + jobID + ".webm",
		IdempotencyKey: idempotencyKey,
		MaxAttempts:    3,
	}
	if err := Enqueue(ctx, database, job); err != nil {
		t.Fatal(err)
	}
	duplicate := job
	duplicate.ID = uuid.NewString()
	if err := Enqueue(ctx, database, duplicate); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE idempotency_key = $1`, idempotencyKey).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotent enqueue count=%d err=%v", count, err)
	}

	first, found, err := store.Claim(ctx, "worker-one", []string{KindMediaDelete}, time.Minute)
	if err != nil || !found || first.ID != jobID || first.Attempts != 1 {
		t.Fatalf("first claim=%+v found=%t err=%v", first, found, err)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan bool, 2)
	for _, owner := range []string{"worker-two", "worker-three"} {
		owner := owner
		go func() {
			defer wait.Done()
			_, claimed, claimErr := store.Claim(ctx, owner, []string{KindMediaDelete}, time.Minute)
			if claimErr != nil {
				t.Errorf("concurrent claim for %s: %v", jobID, claimErr)
			}
			results <- claimed
		}()
	}
	wait.Wait()
	close(results)
	for claimed := range results {
		if claimed {
			t.Fatal("active lease allowed a duplicate claim")
		}
	}

	if _, err := database.Exec(ctx, `UPDATE processing_jobs SET lease_expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	second, found, err := store.Claim(ctx, "worker-two", []string{KindMediaDelete}, time.Minute)
	if err != nil || !found || second.Attempts != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("recovered claim=%+v found=%t err=%v", second, found, err)
	}
	if err := store.Heartbeat(ctx, first, time.Minute); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old lease heartbeat error=%v", err)
	}
	if err := store.Complete(ctx, first); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old lease completion error=%v", err)
	}
	if err := store.Complete(ctx, second); err != nil {
		t.Fatalf("complete recovered job: %v", err)
	}
	var state string
	var attempts int
	if err := database.QueryRow(ctx, `SELECT state, attempts FROM processing_jobs WHERE id = $1`, jobID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "succeeded" || attempts != 2 {
		t.Fatalf("final state=%q attempts=%d", state, attempts)
	}
}
