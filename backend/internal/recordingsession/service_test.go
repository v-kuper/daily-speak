package recordingsession

import (
	"context"
	"errors"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/quota"
)

func TestServiceStartNormalizesPhotoSession(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, &fakeFiles{}, func() string { return "session-id" })
	timestamp := time.Date(2026, 9, 27, 10, 0, 0, 0, time.FixedZone("test", 2*60*60))

	id, err := service.Start(context.Background(), "user-id", StartInput{
		Duration: 32, Timestamp: timestamp, PracticeType: "photo_description",
		PhotoDataURL: "data:image/png;base64,aGVsbG8=", PhotoObject: "  a street  ",
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if id != "session-id" {
		t.Fatalf("expected generated session id, got %q", id)
	}
	if repository.started.Topic != "Photo description" || repository.started.PracticeType != "photo_description" {
		t.Fatalf("unexpected normalized command: %#v", repository.started)
	}
	if repository.started.PhotoDataURL == nil || repository.started.PhotoObject == nil {
		t.Fatalf("expected normalized photo fields: %#v", repository.started)
	}
	if !repository.started.Timestamp.Equal(timestamp.UTC()) {
		t.Fatalf("expected UTC timestamp, got %s", repository.started.Timestamp)
	}
}

func TestServiceSaveChunkRejectsFormatChangeBeforeWriting(t *testing.T) {
	extension := "webm"
	repository := &fakeRepository{sessionFound: true, session: Session{ID: "session-id", Status: "open", AudioExtension: &extension}}
	files := &fakeFiles{}
	service := NewService(repository, files, func() string { return "unused" })

	err := service.SaveChunk(context.Background(), "user-id", "session-id", 1, "m4a", []byte("audio"))
	if !errors.Is(err, ErrFormatChange) {
		t.Fatalf("expected format-change error, got %v", err)
	}
	if files.savedChunk {
		t.Fatal("format mismatch must not write a chunk")
	}
}

func TestServiceFinalizePublishesAndCommitsRecording(t *testing.T) {
	extension := "webm"
	remainingBefore := 100
	remainingAfter := 70
	session := Session{
		ID: "session-id", UserID: "user-id", Topic: "Free talk", Duration: 30,
		Timestamp:    time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC),
		PracticeType: "free_talk", AudioExtension: &extension, ChunkCount: 3, Status: "open",
	}
	created := Recording{ID: "recording-id", Topic: session.Topic, Duration: 30, Timestamp: session.Timestamp}
	repository := &fakeRepository{
		sessionFound: true, session: session,
		quotaResults: []quota.RecordingQuota{
			{WeeklyRemainingSeconds: &remainingBefore},
			{WeeklyRemainingSeconds: &remainingAfter, WeeklyUsedSeconds: 30},
		},
		transaction: &fakeFinalizeTransaction{sessionFound: true, session: session, quota: quota.RecordingQuota{WeeklyRemainingSeconds: &remainingBefore}, created: created, marked: true},
	}
	files := &fakeFiles{finalExists: true, publishedURL: "/uploads/recordings/user-id/recording-id.webm"}
	ids := []string{"recording-id", "job-id"}
	service := NewService(repository, files, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})

	result, err := service.Finalize(context.Background(), "user-id", false, "session-id", FinalizeInput{})
	if err != nil {
		t.Fatalf("finalize session: %v", err)
	}
	if !result.Created || result.Recording.ID != "recording-id" {
		t.Fatalf("unexpected finalize result: %#v", result)
	}
	if result.Quota == nil || result.Quota.WeeklyRemainingSeconds == nil || *result.Quota.WeeklyRemainingSeconds != 70 {
		t.Fatalf("expected refreshed quota, got %#v", result.Quota)
	}
	if files.publishSessionID != "session-id" || files.publishChunks != 3 {
		t.Fatalf("unexpected publication request: %#v", files)
	}
	if files.discardedURL != "" {
		t.Fatalf("committed audio must not be discarded, got %q", files.discardedURL)
	}
	if files.removedSession != "session-id" {
		t.Fatalf("temporary session was not removed, got %q", files.removedSession)
	}
	if repository.transaction.inserted.AudioURL != files.publishedURL || !repository.transaction.enqueued {
		t.Fatalf("transaction did not persist and enqueue the publication: %#v", repository.transaction)
	}
}

func TestServiceFinalizeDiscardsPublishedAudioWhenTransactionFails(t *testing.T) {
	extension := "webm"
	remaining := 100
	session := Session{ID: "session-id", UserID: "user-id", Duration: 30, AudioExtension: &extension, Status: "open"}
	wanted := errors.New("database unavailable")
	repository := &fakeRepository{
		sessionFound: true, session: session,
		quotaResults: []quota.RecordingQuota{{WeeklyRemainingSeconds: &remaining}},
		executeErr:   wanted,
	}
	files := &fakeFiles{finalExists: true, publishedURL: "/uploads/recordings/user-id/recording-id.webm"}
	service := NewService(repository, files, sequentialIDs("recording-id", "job-id"))

	_, err := service.Finalize(context.Background(), "user-id", false, "session-id", FinalizeInput{})
	if !errors.Is(err, wanted) {
		t.Fatalf("expected transaction error, got %v", err)
	}
	if files.discardedURL != files.publishedURL {
		t.Fatalf("expected failed publication cleanup, got %q", files.discardedURL)
	}
	if files.removedSession != "" {
		t.Fatal("retryable temporary session must remain after failed transaction")
	}
}

func TestServiceFinalizeReturnsExistingRecordingWithoutRepublishing(t *testing.T) {
	recordingID := "recording-id"
	session := Session{ID: "session-id", Status: "finalized", RecordingID: &recordingID}
	existing := Recording{ID: recordingID, Topic: "Existing"}
	repository := &fakeRepository{sessionFound: true, session: session, recordingFound: true, recording: existing}
	files := &fakeFiles{}
	service := NewService(repository, files, func() string { return "unused" })

	result, err := service.Finalize(context.Background(), "user-id", false, "session-id", FinalizeInput{})
	if err != nil {
		t.Fatalf("return existing finalization: %v", err)
	}
	if result.Created || result.Recording.ID != recordingID {
		t.Fatalf("unexpected existing result: %#v", result)
	}
	if files.publishSessionID != "" {
		t.Fatal("an idempotent retry must not publish another file")
	}
}

type fakeRepository struct {
	started        StartCommand
	session        Session
	sessionFound   bool
	recording      Recording
	recordingFound bool
	quotaResults   []quota.RecordingQuota
	transaction    *fakeFinalizeTransaction
	executeErr     error
}

func (repository *fakeRepository) Create(_ context.Context, command StartCommand) error {
	repository.started = command
	return nil
}

func (repository *fakeRepository) Load(context.Context, string, string) (Session, bool, error) {
	return repository.session, repository.sessionFound, nil
}

func (repository *fakeRepository) UpdateChunk(context.Context, string, string, string, int) error {
	return nil
}

func (repository *fakeRepository) UpdateFinal(context.Context, string, string, string) error {
	return nil
}

func (repository *fakeRepository) GetQuota(context.Context, string, bool) (quota.RecordingQuota, error) {
	if len(repository.quotaResults) == 0 {
		return quota.RecordingQuota{}, nil
	}
	result := repository.quotaResults[0]
	if len(repository.quotaResults) > 1 {
		repository.quotaResults = repository.quotaResults[1:]
	}
	return result, nil
}

func (repository *fakeRepository) LoadRecording(context.Context, string, string) (Recording, bool, error) {
	return repository.recording, repository.recordingFound, nil
}

func (repository *fakeRepository) ExecuteFinalize(ctx context.Context, operation func(FinalizeTransaction) error) error {
	if repository.executeErr != nil {
		return repository.executeErr
	}
	return operation(repository.transaction)
}

type fakeFinalizeTransaction struct {
	session      Session
	sessionFound bool
	quota        quota.RecordingQuota
	created      Recording
	marked       bool
	inserted     FinalizeCommand
	enqueued     bool
}

func (transaction *fakeFinalizeTransaction) LockSession(context.Context, string, string) (Session, bool, error) {
	return transaction.session, transaction.sessionFound, nil
}

func (transaction *fakeFinalizeTransaction) LockQuota(context.Context, string, time.Time) (quota.RecordingQuota, error) {
	return transaction.quota, nil
}

func (transaction *fakeFinalizeTransaction) InsertRecording(_ context.Context, command FinalizeCommand) (Recording, error) {
	transaction.inserted = command
	return transaction.created, nil
}

func (transaction *fakeFinalizeTransaction) MarkFinalized(context.Context, string, string, string) (bool, error) {
	return transaction.marked, nil
}

func (transaction *fakeFinalizeTransaction) EnqueueProcessing(_ context.Context, command FinalizeCommand) error {
	transaction.enqueued = true
	transaction.inserted = command
	return nil
}

type fakeFiles struct {
	savedChunk       bool
	finalExists      bool
	publishedURL     string
	publishSessionID string
	publishChunks    int
	discardedURL     string
	removedSession   string
}

func (files *fakeFiles) SaveChunk(string, int, string, []byte) error {
	files.savedChunk = true
	return nil
}

func (files *fakeFiles) SaveFinal(string, string, []byte) error { return nil }

func (files *fakeFiles) FinalExists(string, string) (bool, error) { return files.finalExists, nil }

func (files *fakeFiles) Publish(sessionID string, _ string, _ string, _ string, expectedChunks int) (string, error) {
	files.publishSessionID = sessionID
	files.publishChunks = expectedChunks
	return files.publishedURL, nil
}

func (files *fakeFiles) DiscardPublished(publicURL string) error {
	files.discardedURL = publicURL
	return nil
}

func (files *fakeFiles) Remove(sessionID string) error {
	files.removedSession = sessionID
	return nil
}

func sequentialIDs(values ...string) func() string {
	return func() string {
		value := values[0]
		values = values[1:]
		return value
	}
}
