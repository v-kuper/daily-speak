package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

func TestDeleteRecordingQueuesAndRemovesPrivateMediaAsset(t *testing.T) {
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
	t.Cleanup(func() { _, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID) })

	tokenConfig := auth.TokenConfig{SigningKey: []byte(strings.Repeat("recording-deletion-secret-", 2))}
	grant, err := auth.LoginIdentityUser(ctx, database, tokenConfig, auth.Credentials{Email: user.Email, Password: "password123"}, nil, auth.DeviceInfo{Name: "Deletion test", Platform: "test"})
	if err != nil {
		t.Fatalf("create test identity: %v", err)
	}
	mediaStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("private recording audio")
	digest := sha256.Sum256(body)
	checksum := hex.EncodeToString(digest[:])
	assetID := uuid.NewString()
	objectKey := "recordings/" + user.ID + "/" + assetID + ".webm"
	if _, err := mediaStore.Put(ctx, storage.PutRequest{
		Key: objectKey, ContentType: "audio/webm", Size: int64(len(body)), SHA256: checksum,
	}, bytes.NewReader(body)); err != nil {
		t.Fatalf("store media: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, verified_size_bytes, expected_checksum_sha256,
		   verified_checksum_sha256, verified_at, attached_at)
		VALUES ($1, $2, 'recording_audio', 'ready', 'local', $3, 'audio/webm',
		        $4, $4, $5, $5, NOW(), NOW())`, assetID, user.ID, objectKey, len(body), checksum); err != nil {
		t.Fatalf("insert media asset: %v", err)
	}
	recordingID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, status, shadowing_status, audio_asset_id)
		VALUES ($1, $2, 'Deletion test', 30, $3, 'Test transcript', 'ready', 'pending', $4)`,
		recordingID, user.ID, time.Now().UTC(), assetID); err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	server := newTestServer(Config{DB: database, IdentityTokens: tokenConfig, MediaStore: mediaStore})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/recordings/"+recordingID, nil)
	request.Header.Set("Authorization", "Bearer "+grant.AccessToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d: %s", response.Code, response.Body.String())
	}
	assertRecordingCount(t, database, recordingID, 0)

	jobStore := testJobStore(t, server)
	job, found, err := jobStore.Claim(ctx, "deletion-test", []string{workqueue.KindMediaDelete}, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim deletion job: found=%t err=%v", found, err)
	}
	if err := testBackgroundRuntime(t, server).Handle(ctx, job); err != nil {
		t.Fatalf("delete media asset: %v", err)
	}
	if err := jobStore.Complete(ctx, job); err != nil {
		t.Fatalf("complete deletion job: %v", err)
	}
	if _, err := mediaStore.Stat(ctx, objectKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted object stat error = %v", err)
	}
	var state string
	if err := database.QueryRow(ctx, `SELECT state FROM media_assets WHERE id = $1`, assetID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" {
		t.Fatalf("media asset state = %q", state)
	}
}

func assertRecordingCount(t *testing.T, database *db.DB, recordingID string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow(context.Background(), `SELECT COUNT(*) FROM recordings WHERE id = $1`, recordingID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("recording count = %d, want %d", count, expected)
	}
}
