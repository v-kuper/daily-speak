package db

import (
	"strings"
	"testing"
)

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
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
