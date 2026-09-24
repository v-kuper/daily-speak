package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"testing"

	"daily-speaking-practice/backend/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationCatalog(t *testing.T) {
	catalog, err := migrations.All()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(catalog) == 0 || catalog[0].Name != "0001_init.sql" {
		t.Fatalf("unexpected first migration: %+v", catalog)
	}
	for index, migration := range catalog {
		if migration.SQL == "" {
			t.Fatalf("empty migration %s", migration.Name)
		}
		checksum := sha256.Sum256([]byte(migration.SQL))
		if migration.Checksum != hex.EncodeToString(checksum[:]) {
			t.Fatalf("incorrect checksum for %s", migration.Name)
		}
		if index > 0 && catalog[index-1].Name >= migration.Name {
			t.Fatalf("migrations out of order: %s, %s", catalog[index-1].Name, migration.Name)
		}
	}
}

func TestMigrateConcurrentAndAdoptsLegacySchema(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	for _, legacy := range []bool{false, true} {
		name := "new database"
		if legacy {
			name = "legacy database with user data"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			database := isolatedMigrationDatabase(t, databaseURL)
			userID := uuid.NewString()
			if legacy {
				if _, err := database.Exec(ctx, InitialSchemaSQL()); err != nil {
					t.Fatalf("install legacy schema: %v", err)
				}
				if _, err := database.Exec(ctx, `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'test-hash')`, userID, userID+"@example.com"); err != nil {
					t.Fatalf("insert legacy user: %v", err)
				}
			}
			var wg sync.WaitGroup
			failures := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					failures <- database.Migrate(ctx)
				}()
			}
			wg.Wait()
			close(failures)
			for err := range failures {
				if err != nil {
					t.Fatalf("concurrent migration: %v", err)
				}
			}
			var count int
			var checksum string
			if err := database.QueryRow(ctx, `SELECT count(*), max(checksum) FROM schema_migrations WHERE name = $1`, "0001_init.sql").Scan(&count, &checksum); err != nil {
				t.Fatalf("read migration ledger: %v", err)
			}
			if count != 1 || checksum != SchemaHash() {
				t.Fatalf("baseline ledger: count=%d checksum=%q", count, checksum)
			}
			if legacy {
				var retained int
				if err := database.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&retained); err != nil || retained != 1 {
					t.Fatalf("legacy user was not retained: count=%d err=%v", retained, err)
				}
				if _, err := database.Exec(ctx, `UPDATE schema_migrations SET checksum = 'tampered' WHERE name = '0001_init.sql'`); err != nil {
					t.Fatalf("tamper test migration ledger: %v", err)
				}
				if err := database.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
					t.Fatalf("expected checksum mismatch, got %v", err)
				}
			}
		})
	}
}

func isolatedMigrationDatabase(t *testing.T, databaseURL string) *DB {
	t.Helper()
	ctx := context.Background()
	root, err := Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(root.Close)
	schema := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := root.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop isolated test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect isolated test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping isolated test schema: %v", err)
	}
	return &DB{pool: pool}
}

func TestPendingMigrations(t *testing.T) {
	catalog := []migrations.Migration{
		{Name: "0001_init.sql", Checksum: "first"},
		{Name: "0002_next.sql", Checksum: "second"},
	}
	cases := []struct {
		name    string
		applied map[string]string
		want    int
		fail    bool
	}{
		{name: "new database", applied: map[string]string{}, want: 2},
		{name: "baseline installed", applied: map[string]string{"0001_init.sql": "first"}, want: 1},
		{name: "up to date", applied: map[string]string{"0001_init.sql": "first", "0002_next.sql": "second"}},
		{name: "modified migration", applied: map[string]string{"0001_init.sql": "other"}, fail: true},
		{name: "gap", applied: map[string]string{"0002_next.sql": "second"}, fail: true},
		{name: "unknown migration", applied: map[string]string{"0003_future.sql": "third"}, fail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending, err := pendingMigrations(catalog, tc.applied)
			if (err != nil) != tc.fail {
				t.Fatalf("pendingMigrations() error = %v, want failure %v", err, tc.fail)
			}
			if !tc.fail && len(pending) != tc.want {
				t.Fatalf("pending migrations = %d, want %d", len(pending), tc.want)
			}
		})
	}
}

func TestInitialMigrationContainsCurrentTables(t *testing.T) {
	sql := InitialSchemaSQL()
	required := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"CREATE TABLE IF NOT EXISTS user_sessions",
		"CREATE TABLE IF NOT EXISTS user_interests",
		"CREATE TABLE IF NOT EXISTS recordings",
		"CREATE TABLE IF NOT EXISTS recording_upload_sessions",
		"CREATE TABLE IF NOT EXISTS feed_posts",
		"CREATE TABLE IF NOT EXISTS feed_replies",
		"CREATE TABLE IF NOT EXISTS feed_post_reactions",
		"CREATE TABLE IF NOT EXISTS feed_reply_reactions",
		"CREATE TABLE IF NOT EXISTS pending_file_deletions",
		"FOREIGN KEY (source_recording_id) REFERENCES recordings(id) ON DELETE CASCADE",
		"ADD COLUMN IF NOT EXISTS corrected_transcript TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS processing_stage TEXT",
		"ADD COLUMN IF NOT EXISTS shadowing_status TEXT NOT NULL DEFAULT 'pending'",
		"ADD COLUMN IF NOT EXISTS shadowing_audio_url TEXT",
		"ADD COLUMN IF NOT EXISTS shadowing_error TEXT",
		"ADD COLUMN IF NOT EXISTS shadowing_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"ADD COLUMN IF NOT EXISTS shadowing_attempt_id TEXT",
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
