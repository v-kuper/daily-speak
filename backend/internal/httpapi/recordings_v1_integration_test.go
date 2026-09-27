package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestV1RecordingsCursorPagination(t *testing.T) {
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

	email := fmt.Sprintf("v1-page-%s@example.com", uuid.NewString())
	user, err := auth.RegisterUser(ctx, database, email, "password123")
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})
	tokenConfig := auth.TokenConfig{SigningKey: []byte(strings.Repeat("recording-integration-secret-", 2))}
	mobileGrant, err := auth.LoginIdentityUser(ctx, database, tokenConfig, auth.Credentials{Email: email, Password: "password123"}, nil, auth.DeviceInfo{Name: "Integration device", Platform: "ios"})
	if err != nil {
		t.Fatalf("create identity session: %v", err)
	}

	newest := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	recordings := []struct {
		id        string
		timestamp time.Time
	}{
		{id: "page-c-" + uuid.NewString(), timestamp: newest.Add(-time.Minute)},
		{id: "page-a-" + uuid.NewString(), timestamp: newest},
		{id: "page-b-" + uuid.NewString(), timestamp: newest},
	}
	for _, recording := range recordings {
		if _, err := database.Exec(ctx, `
			INSERT INTO recordings (id, user_id, topic, duration, timestamp, transcript)
			VALUES ($1, $2, 'Pagination test', 30, $3, 'Test transcript')`, recording.id, user.ID, recording.timestamp); err != nil {
			t.Fatalf("insert recording: %v", err)
		}
	}
	expectedFirst, expectedSecond := recordings[1].id, recordings[2].id
	if expectedFirst < expectedSecond {
		expectedFirst, expectedSecond = expectedSecond, expectedFirst
	}

	handler := newTestServer(Config{DB: database, IdentityTokens: tokenConfig}).Handler()
	first := requestRecordingPage(t, handler, mobileGrant.AccessToken, "/api/v1/recordings?limit=2")
	if len(first.Items) != 2 || first.Items[0].ID != expectedFirst || first.Items[1].ID != expectedSecond {
		t.Fatalf("unexpected first page: %+v", first.Items)
	}
	if first.Page.NextCursor == nil || *first.Page.NextCursor == "" {
		t.Fatal("first page is missing next cursor")
	}

	second := requestRecordingPage(t, handler, mobileGrant.AccessToken, "/api/v1/recordings?limit=2&cursor="+url.QueryEscape(*first.Page.NextCursor))
	if len(second.Items) != 1 || second.Items[0].ID != recordings[0].id {
		t.Fatalf("unexpected second page: %+v", second.Items)
	}
	if second.Page.NextCursor != nil {
		t.Fatalf("last page has next cursor %q", *second.Page.NextCursor)
	}

	bearerHandler := newTestServer(Config{DB: database, IdentityTokens: tokenConfig}).Handler()
	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/recordings?limit=1", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+mobileGrant.AccessToken)
	bearerResponse := httptest.NewRecorder()
	bearerHandler.ServeHTTP(bearerResponse, bearerRequest)
	if bearerResponse.Code != http.StatusOK {
		t.Fatalf("bearer recordings request: status %d: %s", bearerResponse.Code, bearerResponse.Body.String())
	}
}

func requestRecordingPage(t *testing.T, handler http.Handler, accessToken string, target string) struct {
	Items []recordingResponse `json:"items"`
	Page  pageInfo            `json:"page"`
} {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", target, response.Code, response.Body.String())
	}
	var payload struct {
		Items []recordingResponse `json:"items"`
		Page  pageInfo            `json:"page"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	return payload
}
