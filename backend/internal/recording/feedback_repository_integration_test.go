package recording

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
)

func TestSQLRecordingFeedbackPersistsIndependentWorkAndFencesStaleJobs(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewSQLProcessingRepository(database)

	t.Run("analysis and good-example scheduling commit together", func(t *testing.T) {
		fixture := newInterviewCompletionFixture(t, database)
		analysis := AnalysisResult{Suggestions: []Suggestion{{ID: "correction-1", Wrong: "go", Right: "went", Explanation: "Use past simple.", Span: &FeedbackSpan{Start: 2, End: 4, TurnSequence: 1}}}, StrengthsStatus: "pending"}
		saved, err := repository.SaveAnalysis(ctx, fixture.job, analysis)
		if err != nil || !saved {
			t.Fatalf("saved=%v err=%v", saved, err)
		}
		jobID := fixture.job.ID + ":strengths"
		var kind, state, stage, positiveStatus string
		if err := database.QueryRow(ctx, `SELECT j.kind, j.state, r.processing_stage, r.strengths_status
			FROM recordings r JOIN processing_jobs j ON j.id = r.strengths_job_id WHERE r.id = $1`, fixture.recordingID).
			Scan(&kind, &state, &stage, &positiveStatus); err != nil {
			t.Fatal(err)
		}
		if kind != workqueue.KindRecordingStrengths || state != "queued" || stage != "rewriting" || positiveStatus != "processing" {
			t.Fatalf("kind=%s state=%s stage=%s positives=%s", kind, state, stage, positiveStatus)
		}
		stale := ProcessingJob{ID: jobID, ResourceID: fixture.recordingID, LeaseToken: "old-lease"}
		if err := repository.SaveStrengths(ctx, stale, []Strength{}); !errors.Is(err, workqueue.ErrLeaseLost) {
			t.Fatalf("stale save: %v", err)
		}
		lease := uuid.NewString()
		if _, err := database.Exec(ctx, `UPDATE processing_jobs SET state = 'running', attempts = 1, lease_token = $2, lease_owner = 'feedback-test', lease_expires_at = NOW() + INTERVAL '5 minutes' WHERE id = $1`, jobID, lease); err != nil {
			t.Fatal(err)
		}
		active := ProcessingJob{ID: jobID, ResourceID: fixture.recordingID, LeaseToken: lease}
		if err := repository.SaveStrengths(ctx, active, []Strength{{ID: "strength-1", Excerpt: "I enjoy the museums.", Explanation: "Clear sentence.", Category: CategorySentenceStructure, RuleID: "word-order", Span: &FeedbackSpan{Start: 0, End: 19, TurnSequence: 2}}}); err != nil {
			t.Fatal(err)
		}
		record, found, err := NewSQLQueryRepository(database).Find(ctx, fixture.userID, fixture.recordingID)
		if err != nil || !found || record.Status != "processing" || record.StrengthsStatus != "ready" {
			t.Fatalf("record=%#v found=%v err=%v", record, found, err)
		}
		if strengths := NormalizeStrengths(record.StrengthsJSON, 3); len(strengths) != 1 || strengths[0].Span == nil || strengths[0].Span.TurnSequence != 2 {
			t.Fatalf("strengths=%#v", strengths)
		}
		if _, err := database.Exec(ctx, `UPDATE recordings SET strengths_status = 'processing' WHERE id = $1`, fixture.recordingID); err != nil {
			t.Fatal(err)
		}
		tx, err := database.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := repository.FinalizeStrengthsFailure(ctx, tx, jobID, fixture.recordingID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		record, _, err = NewSQLQueryRepository(database).Find(ctx, fixture.userID, fixture.recordingID)
		if err != nil || record.Status != "processing" || record.StrengthsStatus != "failed" || len(NormalizeSuggestions(record.SuggestionsJSON, 0)) != 1 {
			t.Fatalf("record=%#v err=%v", record, err)
		}
	})

	t.Run("concurrent retry schedules one job and checks ownership", func(t *testing.T) {
		fixture := newInterviewCompletionFixture(t, database)
		if _, err := database.Exec(ctx, `UPDATE recordings SET status = 'ready', strengths_status = 'failed' WHERE id = $1`, fixture.recordingID); err != nil {
			t.Fatal(err)
		}
		service := NewStrengthsService(repository, repository, nil, uuid.NewString)
		if _, err := service.Retry(ctx, "another-owner", fixture.recordingID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ownership: %v", err)
		}
		var wait sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, err := service.Retry(ctx, fixture.userID, fixture.recordingID)
				results <- err
			}()
		}
		wait.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := database.QueryRow(ctx, `SELECT COUNT(*) FROM processing_jobs WHERE resource_id = $1 AND kind = 'recording.strengths'`, fixture.recordingID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("jobs=%d err=%v", count, err)
		}
	})

	t.Run("checkpoint reuse requires matching input and an active lease", func(t *testing.T) {
		fixture := newInterviewCompletionFixture(t, database)
		input := AnalysisInput{RecordingID: fixture.recordingID, Transcript: "I go to Rome.", EnglishLevel: "b1"}
		checkpoint := repository.AnalysisCheckpoint(fixture.job, input)
		candidate := analysisCandidate{Wrong: "go", Right: "went", Category: CategoryVerbGrammar, Span: &FeedbackSpan{Start: 2, End: 4}}
		if err := checkpoint.Save(ctx, CategoryVerbGrammar, []analysisCandidate{candidate}); err != nil {
			t.Fatal(err)
		}
		if got, found, err := checkpoint.Load(ctx, CategoryVerbGrammar); err != nil || !found || len(got) != 1 || got[0].Span.Start != 2 {
			t.Fatalf("got=%#v found=%v err=%v", got, found, err)
		}
		input.EnglishLevel = "b2"
		if _, found, err := repository.AnalysisCheckpoint(fixture.job, input).Load(ctx, CategoryVerbGrammar); err != nil || found {
			t.Fatalf("changed input found=%v err=%v", found, err)
		}
		fixture.job.LeaseToken = "stale"
		if err := repository.AnalysisCheckpoint(fixture.job, input).Save(ctx, CategoryVocabulary, nil); !errors.Is(err, workqueue.ErrLeaseLost) {
			t.Fatalf("stale checkpoint: %v", err)
		}
	})
}
