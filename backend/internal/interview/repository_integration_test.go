package interview

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

func TestInterviewSessionSQLLifecycle(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, url, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	principalID := uuid.NewString()
	_, err = database.Exec(ctx, `INSERT INTO principals(id,kind,expires_at)
		VALUES($1,'guest',$2)`, principalID, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(ctx, `DELETE FROM processing_jobs WHERE resource_id IN
			(SELECT id FROM interview_sessions WHERE owner_principal_id=$1)
			OR resource_id IN (SELECT t.id FROM interview_turns t JOIN interview_sessions s ON s.id=t.session_id
			WHERE s.owner_principal_id=$1)`, principalID)
		_, _ = database.Exec(ctx, `DELETE FROM principals WHERE id=$1`, principalID)
		database.Close()
	})
	repo := NewSQLRepository(database)
	service := NewService(repo)
	input := CreateInput{OwnerPrincipalID: principalID, OwnerKind: "guest", IdempotencyKey: "create-12345678",
		Topic: "Travel", OpeningQuestion: "Travel"}
	created, err := service.Create(ctx, input)
	if err != nil || created.Status != StatusPreparing || created.MaxDurationSeconds != 180 {
		t.Fatalf("create = %+v, err=%v", created, err)
	}
	retried, err := service.Create(ctx, input)
	if err != nil || retried.ID != created.ID {
		t.Fatalf("idempotent create = %+v, err=%v", retried, err)
	}
	changed := input
	changed.Topic = "Food"
	if _, err := service.Create(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key different request err=%v", err)
	}
	leaseToken := uuid.NewString()
	var preparationJob workqueue.Job
	err = database.QueryRow(ctx, `UPDATE processing_jobs
		SET state='running',lease_token=$2,lease_owner='interview-test',
		    lease_expires_at=NOW()+INTERVAL '1 minute',attempts=attempts+1
		WHERE kind=$3 AND resource_id=$1
		RETURNING id,kind,resource_id,lease_token`, created.ID, leaseToken, JobKind).
		Scan(&preparationJob.ID, &preparationJob.Kind, &preparationJob.ResourceID, &preparationJob.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePreparation(ctx, preparationJob, created.ID, Preparation{
		Questions: []string{"Where would you travel first?", "What would you pack for the trip?", "How do you plan your route?"},
		Words:     []string{"journey", "route", "map", "visit", "ticket", "pack", "train", "trip"},
	}); err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(ctx, principalID, created.ID)
	if err != nil || started.CurrentTurnSeq != 1 || len(started.Candidates) != 3 {
		t.Fatalf("start = %+v, err=%v", started, err)
	}
	advance := AdvanceInput{OwnerPrincipalID: principalID, SessionID: created.ID,
		IdempotencyKey: "advance-12345678", CurrentTurnSeq: 1,
		NextCandidateID: started.Candidates[0].ID, AtMs: 1100}
	advanced, err := service.Advance(ctx, advance)
	if err != nil || advanced.CurrentTurnSeq != 2 || advanced.Turns[0].EndedAtMs == nil {
		t.Fatalf("advance = %+v, err=%v", advanced, err)
	}
	retriedAdvance, err := service.Advance(ctx, advance)
	if err != nil || retriedAdvance.CurrentTurnSeq != 2 {
		t.Fatalf("idempotent advance = %+v, err=%v", retriedAdvance, err)
	}
	if _, err := service.Advance(ctx, AdvanceInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, IdempotencyKey: "advance-87654321", CurrentTurnSeq: 1,
		NextCandidateID: started.Candidates[1].ID, AtMs: 1200}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale advance err=%v", err)
	}
	if _, err := database.Exec(ctx, `UPDATE interview_turns
		SET provisional_transcript='batch answer',final_transcript='batch answer',
		    transcript_status='ready',transcript_origin='turn_batch'
		WHERE session_id=$1 AND seq=2`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveTurnTranscript(ctx, SaveTurnTranscriptInput{
		OwnerPrincipalID: principalID, SessionID: created.ID, TurnSeq: 2,
		IdempotencyKey: "transcript-late-1234", Transcript: "late realtime answer",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("late realtime replacement err=%v", err)
	}
	transcriptInput := SaveTurnTranscriptInput{OwnerPrincipalID: principalID, SessionID: created.ID,
		TurnSeq: 1, IdempotencyKey: "transcript-12345678", Transcript: "I enjoy long train journeys."}
	if _, err := service.SaveTurnTranscript(ctx, transcriptInput); err != nil {
		t.Fatalf("save realtime transcript: %v", err)
	}
	if _, err := database.Exec(ctx, `UPDATE interview_sessions SET status='finalizing' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveTurnTranscript(ctx, transcriptInput); err != nil {
		t.Fatalf("idempotent transcript after finalization starts: %v", err)
	}
	changedTranscript := transcriptInput
	changedTranscript.IdempotencyKey = "transcript-87654321"
	if _, err := service.SaveTurnTranscript(ctx, changedTranscript); !errors.Is(err, ErrConflict) {
		t.Fatalf("new transcript after finalization starts err=%v", err)
	}
	if _, err := database.Exec(ctx, `UPDATE interview_sessions SET status='recording' WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(ctx, principalID, created.ID); err != nil {
		t.Fatal(err)
	}
	input.IdempotencyKey = "create-87654321"
	if _, err := service.Create(ctx, input); err != nil {
		t.Fatalf("new interview after cancel: %v", err)
	}
}
