package recording

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryServiceSchedulesFailedStageAtomically(t *testing.T) {
	stage := "suggestions"
	processingError := "failed"
	shadowingAssetID := "old-shadowing-asset"
	records := &recordRepositoryStub{found: true, record: Record{
		ID: "recording-id", Status: "failed", ProcessingStage: &stage,
		Transcript: "I go yesterday.", SuggestionsJSON: []byte("[{\"wrong\":\"go\"}]"),
		CorrectedTranscript: "stale", ProcessingError: &processingError,
		ShadowingStatus: "failed", ShadowingAssetID: &shadowingAssetID,
		InterviewTurns: []InterviewTurn{{Sequence: 1, CorrectedAnswerText: "I went yesterday."}},
	}}
	tx := &retryTransactionStub{claimed: true}
	unit := &retryUnitStub{tx: tx}
	service := NewRetryService(records, unit, func() string { return "job-id" })
	startedAt := time.Date(2026, time.September, 27, 12, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return startedAt }

	result, err := service.Retry(context.Background(), "user-id", "recording-id")
	if err != nil {
		t.Fatalf("retry recording: %v", err)
	}
	if !result.Scheduled || !tx.enqueued || tx.jobID != "job-id" {
		t.Fatalf("retry was not atomically claimed and enqueued: result=%#v tx=%#v", result, tx)
	}
	if result.Record.Status != "processing" || result.Record.ProcessingError != nil {
		t.Fatalf("processing state was not reset: %#v", result.Record)
	}
	if result.Record.Transcript != "I go yesterday." || string(result.Record.SuggestionsJSON) != "[]" {
		t.Fatalf("stage inputs were not preserved correctly: %#v", result.Record)
	}
	if result.Record.CorrectedTranscript != "" || result.Record.ShadowingStatus != "pending" || result.Record.ShadowingAssetID != nil {
		t.Fatalf("downstream state was not cleared: %#v", result.Record)
	}
	if len(result.Record.InterviewTurns) != 1 || result.Record.InterviewTurns[0].CorrectedAnswerText != "" {
		t.Fatalf("per-turn correction was not cleared: %#v", result.Record.InterviewTurns)
	}
	if !result.Record.ShadowingUpdatedAt.Equal(startedAt) {
		t.Fatalf("unexpected shadowing timestamp: %s", result.Record.ShadowingUpdatedAt)
	}
}

func TestRetryServiceTreatsConcurrentClaimAsIdempotent(t *testing.T) {
	stage := "rewriting"
	records := &recordRepositoryStub{found: true, record: Record{
		ID: "recording-id", Status: "failed", ProcessingStage: &stage, Transcript: "I go yesterday.",
	}}
	current := records.record
	current.Status = "processing"
	tx := &retryTransactionStub{currentFound: true, current: current}
	service := NewRetryService(records, &retryUnitStub{tx: tx}, func() string { return "job-id" })

	result, err := service.Retry(context.Background(), "user-id", "recording-id")
	if err != nil {
		t.Fatalf("concurrent retry: %v", err)
	}
	if result.Scheduled || result.Record.Status != "processing" || tx.enqueued {
		t.Fatalf("concurrent claim must return current state without another job: result=%#v tx=%#v", result, tx)
	}
}

func TestRetryServiceValidatesPersistedResumeInputs(t *testing.T) {
	stageTranscribing := "transcribing"
	stageSuggestions := "suggestions"
	assetID := "asset-id"
	tests := []struct {
		name   string
		record Record
		ok     bool
	}{
		{name: "asset transcription", record: Record{Status: "failed", ProcessingStage: &stageTranscribing, AudioAssetID: &assetID}, ok: true},
		{name: "missing transcription media", record: Record{Status: "failed", ProcessingStage: &stageTranscribing}},
		{name: "analysis transcript", record: Record{Status: "failed", ProcessingStage: &stageSuggestions, Transcript: "I go."}, ok: true},
		{name: "missing analysis transcript", record: Record{Status: "failed", ProcessingStage: &stageSuggestions}},
		{name: "ready recording", record: Record{Status: "ready", ProcessingStage: &stageSuggestions, Transcript: "I go."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			records := &recordRepositoryStub{found: true, record: tc.record}
			service := NewRetryService(records, &retryUnitStub{tx: &retryTransactionStub{claimed: true}}, func() string { return "job-id" })
			_, err := service.Retry(context.Background(), "user-id", "recording-id")
			if tc.ok && err != nil {
				t.Fatalf("expected retryable record: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrRetryUnavailable) {
				t.Fatalf("expected unavailable retry, got %v", err)
			}
		})
	}
}

func TestRetryServiceDoesNotOpenTransactionForProcessingRecord(t *testing.T) {
	records := &recordRepositoryStub{found: true, record: Record{ID: "recording-id", Status: "processing"}}
	unit := &retryUnitStub{tx: &retryTransactionStub{}}
	service := NewRetryService(records, unit, func() string { return "unused" })

	result, err := service.Retry(context.Background(), "user-id", "recording-id")
	if err != nil || result.Scheduled || unit.calls != 0 {
		t.Fatalf("unexpected processing retry result: result=%#v err=%v calls=%d", result, err, unit.calls)
	}
}

func TestAfterRetryClaimPreservesOnlyInputsNeededByStage(t *testing.T) {
	startedAt := time.Date(2026, time.September, 27, 12, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		stage           string
		wantTranscript  string
		wantSuggestions string
	}{
		{stage: "transcribing", wantTranscript: "", wantSuggestions: "[]"},
		{stage: "suggestions", wantTranscript: "I go yesterday.", wantSuggestions: "[]"},
		{stage: "rewriting", wantTranscript: "I go yesterday.", wantSuggestions: "[{\"wrong\":\"go\"}]"},
	} {
		t.Run(test.stage, func(t *testing.T) {
			stage := test.stage
			errorMessage := "failed"
			record := Record{
				Status: "failed", ProcessingStage: &stage, ProcessingError: &errorMessage,
				Transcript: "I go yesterday.", SuggestionsJSON: []byte("[{\"wrong\":\"go\"}]"),
				CorrectedTranscript: "stale", ShadowingStatus: "failed", ShadowingError: &errorMessage,
			}

			updated := afterRetryClaim(record, startedAt)
			if updated.Transcript != test.wantTranscript || string(updated.SuggestionsJSON) != test.wantSuggestions {
				t.Fatalf("unexpected stage inputs: %#v", updated)
			}
			if updated.Status != "processing" || updated.ProcessingError != nil || updated.CorrectedTranscript != "" || updated.ShadowingStatus != "pending" || updated.ShadowingError != nil {
				t.Fatalf("downstream state was not reset: %#v", updated)
			}
		})
	}
}

type retryUnitStub struct {
	tx    *retryTransactionStub
	calls int
}

func (unit *retryUnitStub) Execute(ctx context.Context, operation func(RetryTransaction) error) error {
	unit.calls++
	return operation(unit.tx)
}

type retryTransactionStub struct {
	claimed      bool
	current      Record
	currentFound bool
	enqueued     bool
	jobID        string
}

func (transaction *retryTransactionStub) Claim(context.Context, string, string, string) (bool, error) {
	return transaction.claimed, nil
}

func (transaction *retryTransactionStub) Find(context.Context, string, string) (Record, bool, error) {
	return transaction.current, transaction.currentFound, nil
}

func (transaction *retryTransactionStub) Enqueue(_ context.Context, jobID string, _ string) error {
	transaction.enqueued = true
	transaction.jobID = jobID
	return nil
}
