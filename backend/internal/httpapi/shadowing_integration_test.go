package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

type fakeSynthesizer struct {
	mu    sync.Mutex
	calls int
	audio []byte
	err   error
}

func (f *fakeSynthesizer) Synthesize(_ context.Context, _ string) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.audio, f.err
}

func (f *fakeSynthesizer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type shadowingFixture struct {
	database    *db.DB
	owner       auth.User
	ownerCookie *http.Cookie
	recordingID string
	server      *Server
}

func newShadowingFixture(t *testing.T, synthesizer *fakeSynthesizer, correctedTranscript string, shadowingStatus string, updatedAt time.Time) shadowingFixture {
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

	owner, err := auth.RegisterUser(context.Background(), database, fmt.Sprintf("shadow-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() { _, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner.ID) })
	session, err := auth.CreateSession(context.Background(), database, owner.ID)
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	recordingID := uuid.NewString()
	if _, err := database.Exec(context.Background(), `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript, status, shadowing_status, shadowing_updated_at)
		VALUES ($1, $2, 'Shadowing test', 20, NOW(), 'I go yesterday.', $3, 'ready', $4, $5)`,
		recordingID, owner.ID, correctedTranscript, shadowingStatus, updatedAt); err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	uploadsDir := t.TempDir()
	t.Setenv("UPLOADS_DIR", uploadsDir)
	return shadowingFixture{
		database:    database,
		owner:       owner,
		ownerCookie: auth.NewSessionCookie(session.Token, session.ExpiresAt),
		recordingID: recordingID,
		server:      NewServer(Config{DB: database, Synthesizer: synthesizer}),
	}
}

func (f shadowingFixture) post(t *testing.T, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/recordings/"+f.recordingID+"/shadowing", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, request)
	return response
}

func (f shadowingFixture) waitForShadowingStatus(t *testing.T, want string) recordingResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recording, err := f.server.recordingForUser(context.Background(), f.owner.ID, f.recordingID)
		if err == nil && recording.ShadowingStatus == want {
			return recording
		}
		time.Sleep(20 * time.Millisecond)
	}
	recording, err := f.server.recordingForUser(context.Background(), f.owner.ID, f.recordingID)
	t.Fatalf("shadowing status did not become %q: recording=%#v err=%v", want, recording, err)
	return recordingResponse{}
}

func TestShadowingRetryRequiresOwner(t *testing.T) {
	synthesizer := &fakeSynthesizer{audio: []byte("ID3")}
	fixture := newShadowingFixture(t, synthesizer, "I went yesterday.", "pending", time.Now().UTC())
	other, err := auth.RegisterUser(context.Background(), fixture.database, fmt.Sprintf("other-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, other.ID)
	})
	otherSession, err := auth.CreateSession(context.Background(), fixture.database, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.post(t, auth.NewSessionCookie(otherSession.Token, otherSession.ExpiresAt))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if synthesizer.callCount() != 0 {
		t.Fatalf("provider calls=%d", synthesizer.callCount())
	}
}

func TestShadowingRetryRequiresCorrectedTranscript(t *testing.T) {
	synthesizer := &fakeSynthesizer{audio: []byte("ID3")}
	fixture := newShadowingFixture(t, synthesizer, "", "pending", time.Now().UTC())
	response := fixture.post(t, fixture.ownerCookie)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if synthesizer.callCount() != 0 {
		t.Fatalf("provider calls=%d", synthesizer.callCount())
	}
}

func TestShadowingConcurrentRequestsClaimOnce(t *testing.T) {
	synthesizer := &fakeSynthesizer{audio: []byte("ID3")}
	fixture := newShadowingFixture(t, synthesizer, "I went yesterday.", "pending", time.Now().UTC())
	var wait sync.WaitGroup
	wait.Add(2)
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			defer wait.Done()
			statuses <- fixture.post(t, fixture.ownerCookie).Code
		}()
	}
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
	}
	fixture.waitForShadowingStatus(t, "ready")
	if synthesizer.callCount() != 1 {
		t.Fatalf("provider calls=%d", synthesizer.callCount())
	}
}

func TestShadowingRecentProcessingIsNotDuplicated(t *testing.T) {
	synthesizer := &fakeSynthesizer{audio: []byte("ID3")}
	fixture := newShadowingFixture(t, synthesizer, "I went yesterday.", "processing", time.Now().UTC())
	response := fixture.post(t, fixture.ownerCookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if synthesizer.callCount() != 0 {
		t.Fatalf("provider calls=%d", synthesizer.callCount())
	}
}

func TestShadowingStaleProcessingCanBeReclaimed(t *testing.T) {
	synthesizer := &fakeSynthesizer{audio: []byte("ID3")}
	fixture := newShadowingFixture(t, synthesizer, "I went yesterday.", "processing", time.Now().UTC().Add(-6*time.Minute))
	response := fixture.post(t, fixture.ownerCookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	recording := fixture.waitForShadowingStatus(t, "ready")
	if synthesizer.callCount() != 1 || recording.ShadowingAudioURL == nil {
		t.Fatalf("calls=%d recording=%#v", synthesizer.callCount(), recording)
	}
}

func TestShadowingFailurePreservesReadyRecording(t *testing.T) {
	synthesizer := &fakeSynthesizer{err: errors.New("provider body includes secret-internal-detail")}
	fixture := newShadowingFixture(t, synthesizer, "I went yesterday.", "pending", time.Now().UTC())
	response := fixture.post(t, fixture.ownerCookie)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	recording := fixture.waitForShadowingStatus(t, "failed")
	if recording.Status != "ready" || recording.CorrectedTranscript != "I went yesterday." {
		t.Fatalf("main recording was changed: %#v", recording)
	}
	if recording.ShadowingError == nil || strings.Contains(*recording.ShadowingError, "secret-internal-detail") {
		t.Fatalf("unsafe shadowing error: %#v", recording.ShadowingError)
	}
}
