package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/logging"
	"github.com/google/uuid"
)

func TestDeleteRecordingCascadesDataAndRetriesQueuedFilesAfterRestart(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}

	ctx := context.Background()
	database, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	user, err := auth.RegisterUser(ctx, database, fmt.Sprintf("delete-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	recordingID := uuid.NewString()
	postID := uuid.NewString()
	replyID := uuid.NewString()
	uploadSessionID := uuid.NewString()
	recordingURL := fmt.Sprintf("/uploads/recordings/%s/%s.webm", user.ID, recordingID)
	shadowingURL := fmt.Sprintf("/uploads/shadowing/%s/%s.mp3", user.ID, recordingID)
	replyURL := fmt.Sprintf("/uploads/feed-replies/%s/%s.webm", user.ID, replyID)
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM pending_file_deletions WHERE public_url = ANY($1::text[])`, []string{recordingURL, shadowingURL, replyURL})
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})

	session, err := auth.CreateSession(ctx, database, user.ID)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	now := time.Now().UTC()
	if _, err := database.Exec(ctx, `
		INSERT INTO recordings (id, user_id, topic, duration, timestamp, transcript, audio_data_url, status, shadowing_status, shadowing_audio_url)
		VALUES ($1, $2, 'Deletion test', 30, $3, 'Test transcript', $4, 'ready', 'ready', $5)`,
		recordingID, user.ID, now, recordingURL, shadowingURL); err != nil {
		t.Fatalf("insert recording: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO recording_upload_sessions (id, user_id, topic, duration, timestamp, status, recording_id)
		VALUES ($1, $2, 'Deletion test', 30, $3, 'complete', $4)`,
		uploadSessionID, user.ID, now, recordingID); err != nil {
		t.Fatalf("insert upload session: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO feed_posts
		  (id, user_id, source_recording_id, topic, duration, audio_data_url, transcript, source_timestamp)
		VALUES ($1, $2, $3, 'Deletion test', 30, $4, 'Test transcript', $5)`,
		postID, user.ID, recordingID, recordingURL, now); err != nil {
		t.Fatalf("insert feed post: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO feed_replies (id, post_id, user_id, duration, audio_data_url, timestamp)
		VALUES ($1, $2, $3, 5, $4, $5)`,
		replyID, postID, user.ID, replyURL, now); err != nil {
		t.Fatalf("insert feed reply: %v", err)
	}
	if _, err := database.Exec(ctx, `INSERT INTO feed_post_reactions (post_id, user_id, reaction) VALUES ($1, $2, 'like')`, postID, user.ID); err != nil {
		t.Fatalf("insert post reaction: %v", err)
	}
	if _, err := database.Exec(ctx, `INSERT INTO feed_reply_reactions (reply_id, user_id, reaction) VALUES ($1, $2, 'like')`, replyID, user.ID); err != nil {
		t.Fatalf("insert reply reaction: %v", err)
	}

	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	writeTestUpload(t, uploadsDir, recordingURL)
	writeTestUpload(t, uploadsDir, shadowingURL)
	writeTestUpload(t, uploadsDir, replyURL)

	request := httptest.NewRequest(http.MethodDelete, "/api/recordings/"+recordingID, nil)
	request.AddCookie(auth.NewSessionCookie(session.Token, session.ExpiresAt))
	response := httptest.NewRecorder()
	NewServer(Config{DB: database}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d: %s", response.Code, response.Body.String())
	}

	assertTableRowCount(t, database, "recordings", "id", recordingID, 0)
	assertTableRowCount(t, database, "recording_upload_sessions", "id", uploadSessionID, 0)
	assertTableRowCount(t, database, "feed_posts", "id", postID, 0)
	assertTableRowCount(t, database, "feed_replies", "id", replyID, 0)
	assertTableRowCount(t, database, "feed_post_reactions", "post_id", postID, 0)
	assertTableRowCount(t, database, "feed_reply_reactions", "reply_id", replyID, 0)
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", recordingURL, 1)
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", shadowingURL, 1)
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", replyURL, 1)
	if _, err := database.Exec(ctx, `
		INSERT INTO feed_posts
		  (id, user_id, source_recording_id, topic, duration, transcript, source_timestamp)
		VALUES ($1, $2, $3, 'Late publication', 30, 'Late transcript', $4)`,
		uuid.NewString(), user.ID, recordingID, now); err == nil {
		t.Fatal("expected the recording foreign key to reject a late Feed publication")
	}

	restartedServer := NewServer(Config{DB: database})
	restartedServer.removeStoredUploads = func([]string) error {
		return errors.New("simulated Windows sharing violation")
	}
	restartedServer.processPendingFileDeletions(ctx, logging.ForBackground("test.file_cleanup"))
	assertPendingDeletionAttempts(t, database, recordingURL, 1)
	assertPendingDeletionAttempts(t, database, shadowingURL, 1)
	assertPendingDeletionAttempts(t, database, replyURL, 1)

	secondRestart := NewServer(Config{DB: database})
	secondRestart.processPendingFileDeletions(ctx, logging.ForBackground("test.file_cleanup"))
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", recordingURL, 0)
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", shadowingURL, 0)
	assertTableRowCount(t, database, "pending_file_deletions", "public_url", replyURL, 0)
	assertUploadMissing(t, uploadsDir, recordingURL)
	assertUploadMissing(t, uploadsDir, shadowingURL)
	assertUploadMissing(t, uploadsDir, replyURL)
}

func writeTestUpload(t *testing.T, uploadsDir string, publicURL string) {
	t.Helper()
	path := filepath.Join(uploadsDir, filepath.FromSlash(strings.TrimPrefix(publicURL, uploadsURLPrefix)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create upload directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}
}

func assertUploadMissing(t *testing.T, uploadsDir string, publicURL string) {
	t.Helper()
	path := filepath.Join(uploadsDir, filepath.FromSlash(strings.TrimPrefix(publicURL, uploadsURLPrefix)))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected upload %q to be removed, got %v", publicURL, err)
	}
}

func assertTableRowCount(t *testing.T, database *db.DB, table string, column string, value string, expected int) {
	t.Helper()
	allowed := map[string]map[string]bool{
		"recordings":                {"id": true},
		"recording_upload_sessions": {"id": true},
		"feed_posts":                {"id": true},
		"feed_replies":              {"id": true},
		"feed_post_reactions":       {"post_id": true},
		"feed_reply_reactions":      {"reply_id": true},
		"pending_file_deletions":    {"public_url": true},
	}
	if !allowed[table][column] {
		t.Fatalf("unsafe test count target %s.%s", table, column)
	}
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = $1", table, column)
	if err := database.QueryRow(context.Background(), query, value).Scan(&count); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	if count != expected {
		t.Fatalf("expected %d rows in %s for %q, got %d", expected, table, value, count)
	}
}

func assertPendingDeletionAttempts(t *testing.T, database *db.DB, publicURL string, expected int) {
	t.Helper()
	var attempts int
	if err := database.QueryRow(context.Background(), `
		SELECT attempts
		FROM pending_file_deletions
		WHERE public_url = $1`, publicURL).Scan(&attempts); err != nil {
		t.Fatalf("load pending deletion attempts: %v", err)
	}
	if attempts != expected {
		t.Fatalf("expected %d cleanup attempts for %q, got %d", expected, publicURL, attempts)
	}
}
