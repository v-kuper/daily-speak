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
		OpeningUsefulWords: []string{"journey", "explore", "memorable", "abroad", "because", "in my opinion"},
		Candidate: GuidedQuestion{
			Question:    "Where would you travel first?",
			UsefulWords: []string{"destination", "choose", "exciting", "nearby", "because", "for example"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(ctx, principalID, created.ID)
	if err != nil || started.CurrentTurnSeq != 1 || len(started.Candidates) != 1 {
		t.Fatalf("start = %+v, err=%v", started, err)
	}
	if len(started.OpeningUsefulWords) != 6 || len(started.Turns) != 1 ||
		len(started.Turns[0].UsefulWords) != 6 || len(started.Candidates[0].UsefulWords) != 6 {
		t.Fatalf("question guidance = opening:%v turns:%+v candidates:%+v",
			started.OpeningUsefulWords, started.Turns, started.Candidates)
	}
	if _, err := database.Exec(ctx, `INSERT INTO interview_candidates(id,session_id,question,source)
		VALUES($1,$2,'What do you pack for a trip?','prepared')`, uuid.NewString(), created.ID); err == nil {
		t.Fatal("database allowed more than one prepared next question")
	}
	advance := AdvanceInput{OwnerPrincipalID: principalID, SessionID: created.ID,
		IdempotencyKey: "advance-12345678", CurrentTurnSeq: 1,
		NextCandidateID: started.Candidates[0].ID, AtMs: 1100}
	advanced, err := service.Advance(ctx, advance)
	if err != nil || advanced.CurrentTurnSeq != 2 || advanced.Turns[0].EndedAtMs == nil {
		t.Fatalf("advance = %+v, err=%v", advanced, err)
	}
	if len(advanced.Candidates) != 0 || len(advanced.Turns[1].UsefulWords) != 6 ||
		advanced.Turns[1].UsefulWords[0] != "destination" {
		t.Fatalf("candidate words did not follow the visible question: %+v", advanced)
	}
	retriedAdvance, err := service.Advance(ctx, advance)
	if err != nil || retriedAdvance.CurrentTurnSeq != 2 {
		t.Fatalf("idempotent advance = %+v, err=%v", retriedAdvance, err)
	}
	if _, err := service.Advance(ctx, AdvanceInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, IdempotencyKey: "advance-87654321", CurrentTurnSeq: 1,
		NextCandidateID: started.Candidates[0].ID, AtMs: 1200}); !errors.Is(err, ErrConflict) {
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
	secondCandidateID := uuid.NewString()
	if _, err := database.Exec(ctx, `INSERT INTO interview_candidates(id,session_id,question,useful_words,source)
		VALUES($1,$2,'How do you plan your route?','["route","compare","direct","carefully","in advance","for example"]'::jsonb,'adaptive')`,
		secondCandidateID, created.ID); err != nil {
		t.Fatal(err)
	}
	third, err := service.Advance(ctx, AdvanceInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, IdempotencyKey: "advance-third-1234", CurrentTurnSeq: 2,
		NextCandidateID: secondCandidateID, AtMs: 2200})
	if err != nil || third.CurrentTurnSeq != 3 || len(third.Turns) != 3 {
		t.Fatalf("advance before skip = %+v, err=%v", third, err)
	}
	skipped, err := service.SkipTurn(ctx, SkipTurnInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, IdempotencyKey: "skip-turn-12345678", TurnSeq: 3, AtMs: 2500})
	if err != nil || skipped.CurrentTurnSeq != 2 || len(skipped.Turns) != 2 {
		t.Fatalf("skip unanswered turn = %+v, err=%v", skipped, err)
	}
	if _, err := service.SkipTurn(ctx, SkipTurnInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, IdempotencyKey: "skip-turn-12345678", TurnSeq: 3, AtMs: 2500}); err != nil {
		t.Fatalf("idempotent skip: %v", err)
	}
	if _, err := service.AttachAudio(ctx, AttachAudioInput{OwnerPrincipalID: principalID,
		SessionID: created.ID, TurnSeq: 3, AudioAssetID: uuid.NewString(),
		IdempotencyKey: "skipped-audio-1234"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("skipped turn accepted audio: %v", err)
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
