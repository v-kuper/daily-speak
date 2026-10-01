package activity

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func activityDatabase(t *testing.T) *db.DB {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	database, err := db.Connect(context.Background(), url, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestSQLActivityIdempotencyOwnershipOverlapAndMidnight(t *testing.T) {
	database := activityDatabase(t)
	ctx := context.Background()
	user := uuid.NewString()
	other := uuid.NewString()
	for _, id := range []string{user, other} {
		if _, err := database.Exec(ctx, `INSERT INTO users(id,email,password_hash,created_at) VALUES($1,$2,'test','2020-01-01')`, id, id+"@activity.test"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = database.Exec(ctx, `DELETE FROM users WHERE id=ANY($1::text[])`, []string{user, other}) })
	service := NewService(NewSQLRepository(database))
	service.now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	start := time.Date(2026, 10, 1, 20, 59, 50, 0, time.UTC) // Ten seconds before midnight in Minsk.
	input := Interval{ID: "midnight-event", Kind: "speaking", StartedAt: start, EndedAt: start.Add(20 * time.Second)}
	for i := 0; i < 2; i++ {
		if err := service.Record(ctx, user, []Interval{input}); err != nil {
			t.Fatal(err)
		}
	}
	changed := input
	changed.Kind = "review"
	if err := service.Record(ctx, user, []Interval{changed}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if err := service.Record(ctx, other, []Interval{input}); err != nil {
		t.Fatal(err)
	}
	second := Interval{ID: "overlap-event", Kind: "review", StartedAt: start.Add(10 * time.Second), EndedAt: start.Add(30 * time.Second)}
	if err := service.Record(ctx, user, []Interval{second}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Summary(ctx, user, "Europe/Minsk")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalSpeakingMilliseconds != 20_000 {
		t.Fatalf("counter=%d", result.TotalSpeakingMilliseconds)
	}
	byDate := map[string]Day{}
	for _, day := range result.Days {
		byDate[day.Date] = day
	}
	if byDate["2026-10-01"].SpeakingMilliseconds != 10_000 || byDate["2026-10-02"].SpeakingMilliseconds != 10_000 || byDate["2026-10-02"].ReviewMilliseconds != 10_000 {
		t.Fatalf("midnight=%+v %+v", byDate["2026-10-01"], byDate["2026-10-02"])
	}
	// Simultaneous devices are serialized for the account, even with different keys.
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item := input
			item.ID = uuid.NewString()
			item.StartedAt = start.Add(time.Minute)
			item.EndedAt = item.StartedAt.Add(30 * time.Second)
			failures <- service.Record(ctx, user, []Interval{item})
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err = service.Summary(ctx, user, "UTC")
	if err != nil || result.TotalSpeakingMilliseconds != 50_000 {
		t.Fatalf("concurrent total=%d err=%v", result.TotalSpeakingMilliseconds, err)
	}
	foreign, err := service.Summary(ctx, other, "UTC")
	if err != nil || foreign.TotalSpeakingMilliseconds != 20_000 {
		t.Fatalf("other=%+v err=%v", foreign, err)
	}
	// The entire batch rolls back when a key conflicts at its end.
	newItem := input
	newItem.ID = "rollback-event"
	newItem.StartedAt = start.Add(2 * time.Minute)
	newItem.EndedAt = newItem.StartedAt.Add(time.Second)
	if err := service.Record(ctx, user, []Interval{newItem, changed}); !errors.Is(err, ErrConflict) {
		t.Fatalf("batch=%v", err)
	}
	var count int
	_ = database.QueryRow(ctx, `SELECT count(*) FROM activity_events WHERE user_id=$1 AND id=$2`, user, newItem.ID).Scan(&count)
	if count != 0 {
		t.Fatal("failed batch persisted partial activity")
	}

	// Berlin's spring transition makes March 29 a 23-hour calendar day.
	// Its following midnight is 22:00 UTC, not the previous day's 23:00 UTC.
	dstStart := time.Date(2026, 3, 29, 21, 59, 50, 0, time.UTC)
	dst := Interval{ID: "dst-midnight-event", Kind: "review", StartedAt: dstStart, EndedAt: dstStart.Add(20 * time.Second)}
	if err := service.Record(ctx, user, []Interval{dst}); err != nil {
		t.Fatal(err)
	}
	dstSummary, err := service.Summary(ctx, user, "Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range dstSummary.Days {
		if (day.Date == "2026-03-29" || day.Date == "2026-03-30") && day.ReviewMilliseconds != 10_000 {
			t.Fatalf("daylight saving midnight: %+v", day)
		}
	}
}

func TestHistoricalMigrationPreservesTimeAfterRecordingDeletion(t *testing.T) {
	database := activityDatabase(t)
	ctx := context.Background()
	tx, err := database.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	schema := pgx.Identifier{"activity_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := tx.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path TO `+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE users(id TEXT PRIMARY KEY);CREATE TABLE recordings(id TEXT,user_id TEXT,duration INT,timestamp TIMESTAMPTZ);
		INSERT INTO users VALUES('history-user');INSERT INTO recordings VALUES('old-recording','history-user',120,'2025-10-01T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	catalog, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range catalog {
		if migration.Name == "0023_user_activity.sql" {
			if _, err := tx.Exec(ctx, migration.SQL); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM recordings`); err != nil {
		t.Fatal(err)
	}
	var ms int64
	if err := tx.QueryRow(ctx, `SELECT SUM(EXTRACT(EPOCH FROM ended_at-started_at)*1000)::bigint FROM activity_credits WHERE historical`).Scan(&ms); err != nil || ms != 120_000 {
		t.Fatalf("earned=%d err=%v", ms, err)
	}
}
