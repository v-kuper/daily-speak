package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/storage"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

func TestGuestPreviewJourneyIsBoundedIdempotentAndExpiresDurably(t *testing.T) {
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
	tokenConfig := auth.TokenConfig{SigningKey: []byte(strings.Repeat("guest-preview-secret-", 3))}
	guest, err := auth.CreateAnonymousIdentity(ctx, database, tokenConfig, auth.DeviceInfo{Name: "Preview", Platform: "ios"})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM principals WHERE id = $1`, guest.Identity.PrincipalID)
	})
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	audio := []byte("bounded fake audio for injected processors")
	checksum := sha256.Sum256(audio)
	checksumHex := hex.EncodeToString(checksum[:])
	assetID := uuid.NewString()
	objectKey := "v1/" + guest.Identity.PrincipalID + "/guest_preview_audio/fixture.webm"
	if _, err := store.Put(ctx, storage.PutRequest{
		Key: objectKey, ContentType: "audio/webm", Size: int64(len(audio)), SHA256: checksumHex,
	}, bytes.NewReader(audio)); err != nil {
		t.Fatalf("store audio: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, verified_size_bytes, expected_checksum_sha256,
		   verified_checksum_sha256, verified_at, retention_until)
		VALUES ($1, $2, $3, 'ready', 'local', $4, 'audio/webm', $5, $5, $6, $6, NOW(), NOW() + INTERVAL '24 hours')`,
		assetID, guest.Identity.PrincipalID, media.PurposeGuestPreviewAudio, objectKey, len(audio), checksumHex); err != nil {
		t.Fatalf("insert media asset: %v", err)
	}
	client := &countingGuestPreviewAI{response: `{"corrections":[{"wrong":"Yesterday I go","right":"Yesterday I went","explanation":"Use past tense.","category":"verb_grammar","severity":"major","confidence":0.99}]}`}
	server := NewServer(Config{
		DB: database, IdentityTokens: tokenConfig, MediaStore: store, AIClient: client,
		TranscribeAudio:    func(context.Context, string) (string, error) { return "Yesterday I go to work.", nil },
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 31 * time.Second, nil },
	})
	payload := map[string]any{"audioAssetId": assetID, "topic": "My day", "duration": 30, "practiceType": "free_talk"}
	first := postGuestPreview(t, server, guest.AccessToken, "preview-request-1", payload)
	if first.Code != http.StatusCreated {
		t.Fatalf("create preview: status=%d body=%s", first.Code, first.Body.String())
	}
	previewID := decodeGuestPreviewID(t, first)
	retry := postGuestPreview(t, server, guest.AccessToken, "preview-request-1", payload)
	if retry.Code != http.StatusOK || decodeGuestPreviewID(t, retry) != previewID {
		t.Fatalf("idempotent retry: status=%d body=%s", retry.Code, retry.Body.String())
	}
	foreignGuest, err := auth.CreateAnonymousIdentity(ctx, database, tokenConfig, auth.DeviceInfo{Name: "Foreign", Platform: "android"})
	if err != nil {
		t.Fatalf("create foreign guest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM principals WHERE id = $1`, foreignGuest.Identity.PrincipalID)
	})
	foreign := getGuestPreview(t, server, foreignGuest.AccessToken, previewID)
	assertV1ErrorCode(t, foreign, http.StatusNotFound, "not_found")

	var jobID string
	if err := database.QueryRow(ctx, `SELECT preview_job_id FROM guest_previews WHERE id = $1`, previewID).Scan(&jobID); err != nil {
		t.Fatalf("load preview job: %v", err)
	}
	leaseToken := uuid.NewString()
	if _, err := database.Exec(ctx, `
		UPDATE processing_jobs
		SET state = 'running', attempts = 1, lease_token = $2, lease_owner = 'test',
		    lease_expires_at = NOW() + INTERVAL '1 minute'
		WHERE id = $1`, jobID, leaseToken); err != nil {
		t.Fatalf("claim preview job: %v", err)
	}
	if err := server.handleDurableJob(ctx, workqueue.Job{ID: jobID, Kind: workqueue.KindGuestPreview, ResourceID: previewID, LeaseToken: leaseToken}); err != nil {
		t.Fatalf("run preview job: %v", err)
	}
	ready := getGuestPreview(t, server, guest.AccessToken, previewID)
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"state":"ready"`) || !strings.Contains(ready.Body.String(), `"duration":31`) {
		t.Fatalf("ready preview: status=%d body=%s", ready.Code, ready.Body.String())
	}
	if client.calls != 1 || strings.Contains(ready.Body.String(), "correctedTranscript") || strings.Contains(ready.Body.String(), "shadowing") {
		t.Fatalf("guest response leaked full processing or repeated AI: calls=%d body=%s", client.calls, ready.Body.String())
	}
	var expensiveJobs int
	if err := database.QueryRow(ctx, `
		SELECT COUNT(*) FROM processing_jobs
		WHERE resource_id = $1 AND kind IN ('recording.process', 'shadowing.synthesize')`, previewID).Scan(&expensiveJobs); err != nil || expensiveJobs != 0 {
		t.Fatalf("expensive guest jobs=%d err=%v", expensiveJobs, err)
	}

	if _, err := database.Exec(ctx, `
		UPDATE guest_previews
		SET created_at = NOW() - INTERVAL '48 hours', expires_at = NOW() - INTERVAL '1 second'
		WHERE id = $1`, previewID); err != nil {
		t.Fatalf("expire preview: %v", err)
	}
	if err := server.expireGuestPreviews(ctx); err != nil {
		t.Fatalf("expire guest previews: %v", err)
	}
	expired := getGuestPreview(t, server, guest.AccessToken, previewID)
	assertV1ErrorCode(t, expired, http.StatusNotFound, "not_found")
	var assetState string
	if err := database.QueryRow(ctx, `SELECT state FROM media_assets WHERE id = $1`, assetID).Scan(&assetState); err != nil || assetState != "deleting" {
		t.Fatalf("expired asset state=%q err=%v", assetState, err)
	}
	var deleteJobs int
	if err := database.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE resource_id = $1 AND kind = 'media.delete'`, assetID).Scan(&deleteJobs); err != nil || deleteJobs != 1 {
		t.Fatalf("durable delete jobs=%d err=%v", deleteJobs, err)
	}
}

func TestGuestMediaUploadAllowsOneBoundedRecordingOnly(t *testing.T) {
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
	tokenConfig := auth.TokenConfig{SigningKey: []byte(strings.Repeat("guest-upload-secret-", 3))}
	guest, err := auth.CreateAnonymousIdentity(ctx, database, tokenConfig, auth.DeviceInfo{Name: "Upload", Platform: "android"})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM principals WHERE id = $1`, guest.Identity.PrincipalID)
	})
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	server := NewServer(Config{DB: database, IdentityTokens: tokenConfig, MediaStore: store})
	payload := map[string]any{
		"purpose": "recording_audio", "contentType": "audio/webm", "sizeBytes": 1024,
		"checksum": map[string]string{"algorithm": "sha256", "value": strings.Repeat("a", 64)},
	}
	first := postGuestMediaUpload(t, server, guest.AccessToken, "guest-upload-1", payload)
	if first.Code != http.StatusCreated || !strings.Contains(first.Body.String(), `"purpose":"recording_audio"`) {
		t.Fatalf("first guest upload: status=%d body=%s", first.Code, first.Body.String())
	}
	second := postGuestMediaUpload(t, server, guest.AccessToken, "guest-upload-2", payload)
	assertV1ErrorCode(t, second, http.StatusConflict, "conflict")
	photo := map[string]any{
		"purpose": "recording_photo", "contentType": "image/jpeg", "sizeBytes": 100,
		"checksum": map[string]string{"algorithm": "sha256", "value": strings.Repeat("b", 64)},
	}
	denied := postGuestMediaUpload(t, server, guest.AccessToken, "guest-upload-3", photo)
	assertV1ErrorCode(t, denied, http.StatusForbidden, "guest_media_restricted")
}

func postGuestPreview(t *testing.T, server *Server, token, key string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/guest/previews", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func postGuestMediaUpload(t *testing.T, server *Server, token, key string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func getGuestPreview(t *testing.T, server *Server, token, previewID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/guest/previews/%s", previewID), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func decodeGuestPreviewID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Preview struct {
			ID string `json:"id"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Preview.ID == "" {
		t.Fatalf("decode preview response: err=%v body=%s", err, response.Body.String())
	}
	return payload.Preview.ID
}

var _ ai.ChatClient = (*countingGuestPreviewAI)(nil)
