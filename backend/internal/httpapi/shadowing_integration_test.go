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

type controlledSynthesisResult struct {
	audio []byte
	err   error
}

type controlledSynthesizer struct {
	started chan struct{}
	release chan controlledSynthesisResult
}

func newControlledSynthesizer() *controlledSynthesizer {
	return &controlledSynthesizer{
		started: make(chan struct{}),
		release: make(chan controlledSynthesisResult, 1),
	}
}

func (s *controlledSynthesizer) Synthesize(ctx context.Context, _ string) ([]byte, error) {
	close(s.started)
	select {
	case result := <-s.release:
		return result.audio, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
	uploadsDir  string
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
		uploadsDir:  uploadsDir,
		server:      NewServer(Config{DB: database, Synthesizer: synthesizer}),
	}
}

func (f shadowingFixture) getAudio(t *testing.T, cookie *http.Cookie, publicURL string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, publicURL, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, request)
	return response
}

func (f shadowingFixture) post(t *testing.T, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return f.postToServer(t, f.server, cookie)
}

func (f shadowingFixture) postToServer(t *testing.T, server *Server, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/recordings/"+f.recordingID+"/shadowing", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func waitForControlledSynthesizer(t *testing.T, synthesizer *controlledSynthesizer) {
	t.Helper()
	select {
	case <-synthesizer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("synthesizer did not start")
	}
}

func waitForShadowingJobToFinish(t *testing.T, server *Server, recordingID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		server.shadowingProcessingMu.Lock()
		_, running := server.shadowingProcessingJobs[recordingID]
		server.shadowingProcessingMu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shadowing job did not finish")
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

func TestShadowingAudioPlaybackRequiresRecordingOwner(t *testing.T) {
	fixture := newShadowingFixture(t, &fakeSynthesizer{}, "I went yesterday.", "ready", time.Now().UTC())
	publicURL := "/uploads/shadowing/" + fixture.owner.ID + "/" + fixture.recordingID + ".mp3"
	absolutePath := filepath.Join(fixture.uploadsDir, "shadowing", fixture.owner.ID, fixture.recordingID+".mp3")
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolutePath, []byte("ID3-owner-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(context.Background(), `
		UPDATE recordings SET shadowing_audio_url = $2 WHERE id = $1`, fixture.recordingID, publicURL); err != nil {
		t.Fatal(err)
	}

	other, err := auth.RegisterUser(context.Background(), fixture.database, fmt.Sprintf("media-other-%s@example.com", uuid.NewString()), "password123")
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

	nonOwnerResponse := fixture.getAudio(t, auth.NewSessionCookie(otherSession.Token, otherSession.ExpiresAt), publicURL)
	if nonOwnerResponse.Code != http.StatusNotFound {
		t.Fatalf("non-owner status=%d body=%q", nonOwnerResponse.Code, nonOwnerResponse.Body.String())
	}
	ownerResponse := fixture.getAudio(t, fixture.ownerCookie, publicURL)
	if ownerResponse.Code != http.StatusOK || ownerResponse.Body.String() != "ID3-owner-audio" {
		t.Fatalf("owner status=%d body=%q", ownerResponse.Code, ownerResponse.Body.String())
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

func TestReclaimedShadowingIgnoresOlderWorkerFailure(t *testing.T) {
	oldSynthesizer := newControlledSynthesizer()
	fixture := newShadowingFixture(t, &fakeSynthesizer{}, "I went yesterday.", "pending", time.Now().UTC())
	oldServer := NewServer(Config{DB: fixture.database, Synthesizer: oldSynthesizer})
	if response := fixture.postToServer(t, oldServer, fixture.ownerCookie); response.Code != http.StatusOK {
		t.Fatalf("old claim status=%d body=%q", response.Code, response.Body.String())
	}
	waitForControlledSynthesizer(t, oldSynthesizer)
	if _, err := fixture.database.Exec(context.Background(), `
		UPDATE recordings SET shadowing_updated_at = NOW() - INTERVAL '6 minutes' WHERE id = $1`, fixture.recordingID); err != nil {
		t.Fatal(err)
	}

	newSynthesizer := newControlledSynthesizer()
	newServer := NewServer(Config{DB: fixture.database, Synthesizer: newSynthesizer})
	if response := fixture.postToServer(t, newServer, fixture.ownerCookie); response.Code != http.StatusOK {
		t.Fatalf("new claim status=%d body=%q", response.Code, response.Body.String())
	}
	waitForControlledSynthesizer(t, newSynthesizer)

	oldSynthesizer.release <- controlledSynthesisResult{err: errors.New("old worker failed")}
	waitForShadowingJobToFinish(t, oldServer, fixture.recordingID)
	newSynthesizer.release <- controlledSynthesisResult{audio: []byte("ID3-new-attempt")}
	recording := fixture.waitForShadowingStatus(t, "ready")
	if recording.ShadowingAudioURL == nil {
		t.Fatal("new attempt did not publish audio URL")
	}
	absolutePath, err := storedUploadPath(*recording.ShadowingAudioURL)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil || string(data) != "ID3-new-attempt" {
		t.Fatalf("published audio=%q err=%v", data, err)
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
