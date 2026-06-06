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
		"CREATE TABLE IF NOT EXISTS feed_posts",
		"CREATE TABLE IF NOT EXISTS feed_replies",
		"CREATE TABLE IF NOT EXISTS feed_post_reactions",
		"CREATE TABLE IF NOT EXISTS feed_reply_reactions",
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
