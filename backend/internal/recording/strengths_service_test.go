package recording

import (
	"context"
	"errors"
	"testing"
)

type strengthFixture struct {
	source   StrengthsRetrySource
	found    bool
	owner    string
	enqueued int
	work     ProcessingWork
	saved    []Strength
	loadErr  error
}

func (f *strengthFixture) ExecuteStrengths(ctx context.Context, operation func(StrengthsTransaction) error) error {
	return operation(f)
}
func (f *strengthFixture) LockOwned(_ context.Context, owner, _ string) (StrengthsRetrySource, bool, error) {
	f.owner = owner
	return f.source, f.found, nil
}
func (f *strengthFixture) Enqueue(context.Context, string, string) error {
	f.enqueued++
	f.source.StrengthsStatus = "processing"
	return nil
}
func (f *strengthFixture) LoadStrengthsWork(context.Context, ProcessingJob) (ProcessingWork, bool, error) {
	return f.work, f.found, f.loadErr
}
func (f *strengthFixture) SaveStrengths(_ context.Context, _ ProcessingJob, data []Strength) error {
	f.saved = data
	return nil
}

type strengthAnalyzer struct {
	err   error
	calls int
}

func (a *strengthAnalyzer) AnalyzeStrengths(context.Context, AnalysisInput, []Suggestion, AnalysisLogger) ([]Strength, error) {
	a.calls++
	return []Strength{{Excerpt: "after work"}}, a.err
}

func TestStrengthRetryAuthorizesAndSchedulesOnlyEligibleWork(t *testing.T) {
	for _, test := range []struct {
		name     string
		source   StrengthsRetrySource
		found    bool
		expected error
		count    int
	}{
		{"not owned", StrengthsRetrySource{}, false, ErrNotFound, 0},
		{"transcribing", StrengthsRetrySource{Status: "processing", Stage: "transcribing"}, true, ErrStrengthsUnavailable, 0},
		{"failed analysis", StrengthsRetrySource{Status: "failed", Stage: "suggestions", Transcript: "I go home."}, true, ErrStrengthsUnavailable, 0},
		{"ready", StrengthsRetrySource{Status: "ready", Transcript: "I went home.", StrengthsStatus: "failed"}, true, nil, 1},
		{"rewrite failed", StrengthsRetrySource{Status: "failed", Stage: "rewriting", Transcript: "I went home.", StrengthsStatus: "failed"}, true, nil, 1},
		{"already running", StrengthsRetrySource{Status: "ready", Transcript: "I went home.", StrengthsStatus: "processing"}, true, nil, 0},
		{"already complete", StrengthsRetrySource{Status: "ready", Transcript: "I went home.", StrengthsStatus: "ready"}, true, nil, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &strengthFixture{source: test.source, found: test.found}
			service := NewStrengthsService(fixture, fixture, nil, func() string { return "job" })
			scheduled, err := service.Retry(context.Background(), "owner", "recording")
			if !errors.Is(err, test.expected) || fixture.enqueued != test.count || scheduled != (test.count > 0) || fixture.owner != "owner" {
				t.Fatalf("scheduled=%v err=%v fixture=%#v", scheduled, err, fixture)
			}
			if err == nil {
				if _, err := service.Retry(context.Background(), "owner", "recording"); err != nil || fixture.enqueued != test.count {
					t.Fatalf("duplicate scheduling: %v", err)
				}
			}
		})
	}
}

func TestStrengthWorkerFailureDoesNotSaveEmptySuccessAndInactiveJobsDoNoWork(t *testing.T) {
	fixture := &strengthFixture{found: true, work: ProcessingWork{Transcript: "I went home."}}
	analyzer := &strengthAnalyzer{err: ErrAnalysis}
	service := NewStrengthsService(fixture, fixture, analyzer, nil)
	if err := service.Process(context.Background(), ProcessingJob{}, discardAnalysisLogger{}); !errors.Is(err, ErrAnalysis) || fixture.saved != nil {
		t.Fatalf("err=%v saved=%v", err, fixture.saved)
	}
	fixture.found = false
	if err := service.Process(context.Background(), ProcessingJob{}, discardAnalysisLogger{}); err != nil || analyzer.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, analyzer.calls)
	}
	fixture.found = true
	analyzer.err = nil
	if err := service.Process(context.Background(), ProcessingJob{}, discardAnalysisLogger{}); err != nil || len(fixture.saved) != 1 {
		t.Fatalf("err=%v saved=%v", err, fixture.saved)
	}
}
