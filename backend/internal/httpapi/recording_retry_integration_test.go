package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

type retryAIClient struct {
	mu         sync.Mutex
	calls      int
	blockFirst bool
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (client *retryAIClient) PostChat(ctx context.Context, body any) (ai.ChatResponse, error) {
	client.mu.Lock()
	client.calls++
	call := client.calls
	client.mu.Unlock()
	if client.blockFirst && call == 1 {
		client.once.Do(func() { close(client.started) })
		select {
		case <-client.release:
		case <-ctx.Done():
			return ai.ChatResponse{}, ctx.Err()
		}
	}

	prompt := retryPromptFromBody(body)
	switch {
	case strings.Contains(prompt, "adjudicator, not an error detector"):
		return ai.ChatResponse{Response: `{"decisions":{}}`}, nil
	case strings.Contains(prompt, "Rewrite the transcript as natural conversational English"):
		return ai.ChatResponse{Response: `{"correctedTranscript":"I went home yesterday."}`}, nil
	default:
		return ai.ChatResponse{Response: `{"candidates":[]}`}, nil
	}
}

func (client *retryAIClient) callCount() int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.calls
}

func retryPromptFromBody(body any) string {
	payload, _ := body.(map[string]any)
	messages, _ := payload["messages"].([]map[string]string)
	for _, message := range messages {
		if message["role"] == "user" {
			return message["content"]
		}
	}
	return ""
}

type recordingRetryFixture struct {
	database    *db.DB
	owner       auth.User
	ownerCookie *http.Cookie
	recordingID string
	client      *retryAIClient
	server      *Server
}

func newRecordingRetryFixture(t *testing.T, stage string, client *retryAIClient) recordingRetryFixture {
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

	owner, err := auth.RegisterUser(context.Background(), database, fmt.Sprintf("retry-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() { _, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner.ID) })
	session, err := auth.CreateSession(context.Background(), database, owner.ID)
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}

	recordingID := uuid.NewString()
	suggestions := `[]`
	if stage == "rewriting" {
		suggestions = `[{"wrong":"go","right":"went","explanation":"Use past tense.","category":"verb_grammar","severity":"medium"}]`
	}
	if _, err := database.Exec(context.Background(), `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, suggestions, corrected_transcript,
		   status, processing_stage, processing_error, shadowing_status, shadowing_updated_at)
		VALUES ($1, $2, 'Retry test', 20, NOW(), 'I go home yesterday.', $3::jsonb, '',
		        'failed', $4, 'AI suggestions could not be generated.', 'pending', NOW())`,
		recordingID, owner.ID, suggestions, stage); err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	t.Setenv("UPLOADS_DIR", t.TempDir())
	return recordingRetryFixture{
		database:    database,
		owner:       owner,
		ownerCookie: auth.NewSessionCookie(session.Token, session.ExpiresAt),
		recordingID: recordingID,
		client:      client,
		server:      NewServer(Config{DB: database, AIClient: client, Synthesizer: &fakeSynthesizer{audio: []byte("ID3")}}),
	}
}

func (fixture recordingRetryFixture) post(t *testing.T, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/recordings/"+fixture.recordingID+"/retry", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(response, request)
	return response
}

func (fixture recordingRetryFixture) waitForStatus(t *testing.T, want string) recordingResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recording, err := fixture.server.recordingForUser(context.Background(), fixture.owner.ID, fixture.recordingID)
		if err == nil && recording.Status == want {
			return recording
		}
		time.Sleep(20 * time.Millisecond)
	}
	recording, err := fixture.server.recordingForUser(context.Background(), fixture.owner.ID, fixture.recordingID)
	t.Fatalf("recording status did not become %q: recording=%#v err=%v", want, recording, err)
	return recordingResponse{}
}

func (fixture recordingRetryFixture) waitForShadowingStatus(t *testing.T, want string) recordingResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recording, err := fixture.server.recordingForUser(context.Background(), fixture.owner.ID, fixture.recordingID)
		if err == nil && recording.ShadowingStatus == want {
			return recording
		}
		time.Sleep(20 * time.Millisecond)
	}
	recording, err := fixture.server.recordingForUser(context.Background(), fixture.owner.ID, fixture.recordingID)
	t.Fatalf("shadowing status did not become %q: recording=%#v err=%v", want, recording, err)
	return recordingResponse{}
}

func TestRecordingRetryAnalysisClaimsOnceAndContinuesToReady(t *testing.T) {
	client := &retryAIClient{blockFirst: true, started: make(chan struct{}), release: make(chan struct{})}
	fixture := newRecordingRetryFixture(t, "suggestions", client)

	first := fixture.post(t, fixture.ownerCookie)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	firstRecording := decodeRetryRecording(t, first)
	if firstRecording.Status != "processing" || firstRecording.ProcessingStage == nil || *firstRecording.ProcessingStage != "suggestions" {
		t.Fatalf("first recording=%#v", firstRecording)
	}
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("analysis retry did not start")
	}
	second := fixture.post(t, fixture.ownerCookie)
	if second.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	close(client.release)
	recording := fixture.waitForStatus(t, "ready")
	if recording.CorrectedTranscript != "I went home yesterday." || client.callCount() != 9 {
		t.Fatalf("recording=%#v calls=%d", recording, client.callCount())
	}
	fixture.waitForShadowingStatus(t, "ready")
}

func TestRecordingRetryRewriteSkipsDetectorsAndReviewer(t *testing.T) {
	client := &retryAIClient{}
	fixture := newRecordingRetryFixture(t, "rewriting", client)

	response := fixture.post(t, fixture.ownerCookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	recording := fixture.waitForStatus(t, "ready")
	if recording.CorrectedTranscript != "I went home yesterday." || client.callCount() != 1 {
		t.Fatalf("recording=%#v calls=%d", recording, client.callCount())
	}
	fixture.waitForShadowingStatus(t, "ready")
}

func TestRecordingRetryRequiresOwner(t *testing.T) {
	client := &retryAIClient{}
	fixture := newRecordingRetryFixture(t, "suggestions", client)
	other, err := auth.RegisterUser(context.Background(), fixture.database, fmt.Sprintf("retry-other-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, other.ID)
	})
	session, err := auth.CreateSession(context.Background(), fixture.database, other.ID)
	if err != nil {
		t.Fatal(err)
	}

	response := fixture.post(t, auth.NewSessionCookie(session.Token, session.ExpiresAt))
	if response.Code != http.StatusNotFound || client.callCount() != 0 {
		t.Fatalf("status=%d body=%s calls=%d", response.Code, response.Body.String(), client.callCount())
	}
}

func decodeRetryRecording(t *testing.T, response *httptest.ResponseRecorder) recordingResponse {
	t.Helper()
	var payload struct {
		Recording recordingResponse `json:"recording"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Recording
}
