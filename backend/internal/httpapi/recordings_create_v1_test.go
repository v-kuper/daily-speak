package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestNormalizeRecordingCreateV1RequiresPhotoForPhotoPractice(t *testing.T) {
	_, err := normalizeRecordingCreateV1(recordingCreateV1Request{
		Topic: "Describe it", Duration: 30, Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		PracticeType: "photo_description", AudioAssetID: "audio-1",
	})
	if err == nil || !strings.Contains(err.Error(), "Photo asset") {
		t.Fatalf("expected photo validation error, got %v", err)
	}
}

func TestDeterministicRecordingCreateV1Identity(t *testing.T) {
	firstID, firstDigest := deterministicRecordingCreateV1Identity("principal-1", "retry-key", "recording")
	secondID, secondDigest := deterministicRecordingCreateV1Identity("principal-1", "retry-key", "recording")
	otherID, _ := deterministicRecordingCreateV1Identity("principal-1", "other-key", "recording")
	if firstID != secondID || firstDigest != secondDigest {
		t.Fatal("same principal and idempotency key must produce the same identity")
	}
	if firstID == otherID || len(firstDigest) != 64 {
		t.Fatalf("unexpected deterministic identity %q / %q", firstID, firstDigest)
	}
}

func TestRecordingMediaExposesStableBackendPaths(t *testing.T) {
	audioID := "audio asset"
	photoID := "photo-asset"
	media := recordingMedia(&audioID, &photoID, nil)
	if media == nil || media.Audio == nil || media.Photo == nil || media.Shadowing != nil {
		t.Fatalf("unexpected media response: %#v", media)
	}
	if media.Audio.AssetID != audioID || media.Audio.DownloadPath != "/api/v1/media/audio%20asset/download" {
		t.Fatalf("unexpected audio media response: %#v", media.Audio)
	}
	if empty := recordingMedia(nil, nil, nil); empty != nil {
		t.Fatalf("empty media response must be omitted: %#v", empty)
	}
}

func TestCreateRecordingV1AttachesReadyMediaAndIsIdempotent(t *testing.T) {
	fixture := newRecordingCreateV1Fixture(t)
	audioID := fixture.insertAsset(t, fixture.owner.Identity.PrincipalID, "recording_audio")
	photoID := fixture.insertAsset(t, fixture.owner.Identity.PrincipalID, "recording_photo")
	body := map[string]any{
		"topic": "City park", "duration": 42, "timestamp": "2026-09-26T12:30:00.123456Z",
		"practiceType": "photo_description", "audioAssetId": audioID, "photoAssetId": photoID,
	}

	first := fixture.post(t, fixture.owner.AccessToken, "attach-once", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: status %d: %s", first.Code, first.Body.String())
	}
	firstID := decodeCreatedRecordingV1(t, first).ID
	second := fixture.post(t, fixture.owner.AccessToken, "attach-once", body)
	if second.Code != http.StatusCreated {
		t.Fatalf("idempotent create: status %d: %s", second.Code, second.Body.String())
	}
	if secondID := decodeCreatedRecordingV1(t, second).ID; secondID != firstID {
		t.Fatalf("idempotent recording id = %q, want %q", secondID, firstID)
	}

	var count int
	if err := fixture.database.QueryRow(context.Background(), `SELECT COUNT(*) FROM recordings WHERE id = $1`, firstID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recording count = %d, err=%v", count, err)
	}
	if err := fixture.database.QueryRow(context.Background(), `SELECT COUNT(*) FROM processing_jobs WHERE resource_id = $1 AND kind = 'recording.process'`, firstID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("processing job count = %d, err=%v", count, err)
	}
	var attached int
	if err := fixture.database.QueryRow(context.Background(), `SELECT COUNT(*) FROM media_assets WHERE id = ANY($1::text[]) AND attached_at IS NOT NULL`, []string{audioID, photoID}).Scan(&attached); err != nil || attached != 2 {
		t.Fatalf("attached assets = %d, err=%v", attached, err)
	}
	var audioURL, photoURL *string
	if err := fixture.database.QueryRow(context.Background(), `SELECT audio_data_url, photo_data_url FROM recordings WHERE id = $1`, firstID).Scan(&audioURL, &photoURL); err != nil {
		t.Fatalf("load legacy URLs: %v", err)
	}
	if audioURL != nil || photoURL != nil {
		t.Fatalf("legacy URLs must stay null, got audio=%v photo=%v", audioURL, photoURL)
	}
}

func TestCreateRecordingV1RejectsGuestAndForeignMedia(t *testing.T) {
	fixture := newRecordingCreateV1Fixture(t)
	foreignAudioID := fixture.insertAsset(t, fixture.other.Identity.PrincipalID, "recording_audio")
	body := map[string]any{
		"topic": "Foreign", "duration": 20, "timestamp": "2026-09-26T12:30:00Z",
		"practiceType": "free_talk", "audioAssetId": foreignAudioID,
	}

	foreign := fixture.post(t, fixture.owner.AccessToken, "foreign-media", body)
	assertV1ErrorCode(t, foreign, http.StatusNotFound, "media_not_found")
	guest := fixture.post(t, fixture.guest.AccessToken, "guest-recording", body)
	assertV1ErrorCode(t, guest, http.StatusForbidden, "account_required")
}

type recordingCreateV1Fixture struct {
	database *db.DB
	server   *Server
	owner    auth.TokenGrant
	other    auth.TokenGrant
	guest    auth.TokenGrant
}

func newRecordingCreateV1Fixture(t *testing.T) recordingCreateV1Fixture {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	database, err := db.Connect(context.Background(), databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	tokenConfig := auth.TokenConfig{SigningKey: []byte(strings.Repeat("recording-create-v1-secret-", 2))}
	owner := registerRecordingCreateV1User(t, database, tokenConfig, "owner")
	other := registerRecordingCreateV1User(t, database, tokenConfig, "other")
	guest, err := auth.CreateAnonymousIdentity(context.Background(), database, tokenConfig, auth.DeviceInfo{Name: "Guest test", Platform: "ios"})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM principals WHERE id = $1`, guest.Identity.PrincipalID)
	})
	return recordingCreateV1Fixture{
		database: database,
		server:   NewServer(Config{DB: database, IdentityTokens: tokenConfig}),
		owner:    owner,
		other:    other,
		guest:    guest,
	}
}

func registerRecordingCreateV1User(t *testing.T, database *db.DB, tokenConfig auth.TokenConfig, label string) auth.TokenGrant {
	t.Helper()
	email := fmt.Sprintf("recording-create-%s-%s@example.com", label, uuid.NewString())
	user, err := auth.RegisterUser(context.Background(), database, email, "password123")
	if err != nil {
		t.Fatalf("register %s user: %v", label, err)
	}
	t.Cleanup(func() { _, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID) })
	grant, err := auth.LoginMobileUser(context.Background(), database, tokenConfig, auth.Credentials{Email: email, Password: "password123"}, nil, auth.DeviceInfo{Name: label, Platform: "ios"})
	if err != nil {
		t.Fatalf("login %s user: %v", label, err)
	}
	return grant
}

func (f recordingCreateV1Fixture) insertAsset(t *testing.T, principalID string, purpose string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.database.Exec(context.Background(), `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   verified_size_bytes, verified_checksum_sha256, verified_at)
		VALUES
		  ($1, $2, $3, 'ready', 'local', $4, $5, 4, $6, NOW())`,
		id, principalID, purpose, "test/"+id, chooseString(purpose == "recording_photo", "image/jpeg", "audio/webm"), strings.Repeat("a", 64))
	if err != nil {
		t.Fatalf("insert %s asset: %v", purpose, err)
	}
	return id
}

func (f recordingCreateV1Fixture) post(t *testing.T, accessToken string, idempotencyKey string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/recordings", bytes.NewReader(encoded))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	f.server.handleCreateRecordingV1(response, request)
	return response
}

func decodeCreatedRecordingV1(t *testing.T, response *httptest.ResponseRecorder) recordingResponse {
	t.Helper()
	var payload struct {
		Recording recordingResponse `json:"recording"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode recording: %v", err)
	}
	return payload.Recording
}

func assertV1ErrorCode(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var payload v1ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if payload.Error.Code != code {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, code)
	}
}
