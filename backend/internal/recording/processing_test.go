package recording

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type processorRepository struct {
	work               ProcessingWork
	found              bool
	interests          []string
	steps              []string
	duration           int
	transcript         string
	suggestions        []Suggestion
	corrected          string
	shadowingID        string
	advanceTranscript  bool
	advanceSuggestions bool
}

type interviewProcessorRepository struct {
	*processorRepository
	turns         []InterviewTurn
	answers       map[int]string
	verifySeconds int
	verifyMS      int
}

func (r *interviewProcessorRepository) VerifyInterviewDuration(_ context.Context, _ ProcessingJob, _ string, seconds, milliseconds int) error {
	r.steps = append(r.steps, "verify")
	r.verifySeconds = seconds
	r.verifyMS = milliseconds
	return nil
}
func (r *interviewProcessorRepository) LoadInterviewTurns(context.Context, string) ([]InterviewTurn, error) {
	r.steps = append(r.steps, "turns")
	return r.turns, nil
}
func (r *interviewProcessorRepository) SaveInterviewTranscript(_ context.Context, _ ProcessingJob, transcript, _ string, answers map[int]string) (bool, error) {
	r.steps = append(r.steps, "interview_transcript")
	r.transcript, r.answers = transcript, answers
	return r.advanceTranscript, nil
}

func (r *processorRepository) LoadProcessingWork(context.Context, ProcessingJob) (ProcessingWork, bool, error) {
	r.steps = append(r.steps, "load")
	return r.work, r.found, nil
}
func (r *processorRepository) VerifyDuration(_ context.Context, _ ProcessingJob, duration int) error {
	r.steps = append(r.steps, "duration")
	r.duration = duration
	return nil
}
func (r *processorRepository) UserInterests(context.Context, string) ([]string, error) {
	r.steps = append(r.steps, "interests")
	return r.interests, nil
}
func (r *processorRepository) SaveTranscript(_ context.Context, _ ProcessingJob, transcript string) (bool, error) {
	r.steps = append(r.steps, "transcript")
	r.transcript = transcript
	return r.advanceTranscript, nil
}
func (r *processorRepository) SaveSuggestions(_ context.Context, _ ProcessingJob, suggestions []Suggestion) (bool, error) {
	r.steps = append(r.steps, "suggestions")
	r.suggestions = suggestions
	return r.advanceSuggestions, nil
}
func (r *processorRepository) CompleteRecording(_ context.Context, _ ProcessingJob, corrected, shadowingID string) (bool, error) {
	r.steps = append(r.steps, "complete")
	r.corrected = corrected
	r.shadowingID = shadowingID
	return true, nil
}

type processorAnalyzer struct{ inputs []AnalysisInput }

func (a *processorAnalyzer) Analyze(_ context.Context, input AnalysisInput, _ AnalysisLogger) ([]Suggestion, error) {
	a.inputs = append(a.inputs, input)
	return []Suggestion{{Wrong: "go", Right: "went", Explanation: "Use past tense."}}, nil
}

type processorRewriter struct{ inputs []RewriteInput }

func (r *processorRewriter) Rewrite(_ context.Context, input RewriteInput, _ AnalysisLogger) (string, error) {
	r.inputs = append(r.inputs, input)
	return "I went home.", nil
}

type processorMaterializer struct{ cleaned bool }

func (m *processorMaterializer) Materialize(context.Context, string) (string, func(), error) {
	return "/tmp/audio.webm", func() { m.cleaned = true }, nil
}

func TestProcessorRunsDurableStagesInOrder(t *testing.T) {
	repository := &processorRepository{
		found: true,
		work: ProcessingWork{
			UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1"),
			Topic: "My day", PracticeType: "free_talk", EnglishLevel: "b1",
		},
		interests: []string{"travel"}, advanceTranscript: true, advanceSuggestions: true,
	}
	analyzer := &processorAnalyzer{}
	rewriter := &processorRewriter{}
	materializer := &processorMaterializer{}
	processor := NewProcessor(ProcessingDependencies{
		Repository:         repository,
		Materializer:       materializer,
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 30500 * time.Millisecond, nil },
		Transcribe: func(_ context.Context, path string) (string, error) {
			if path != "/tmp/audio.webm" {
				t.Fatalf("path=%q", path)
			}
			return "  I   go home.  ", nil
		},
		Analyzer: analyzer,
		Rewriter: rewriter,
		NewID:    func() string { return "shadowing-job" },
	})
	err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1", LeaseToken: "lease"}, discardAnalysisLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if !materializer.cleaned {
		t.Fatal("materialized audio was not cleaned")
	}
	wantSteps := []string{"load", "duration", "interests", "transcript", "suggestions", "complete"}
	if !reflect.DeepEqual(repository.steps, wantSteps) {
		t.Fatalf("steps=%#v", repository.steps)
	}
	if repository.duration != 31 || repository.transcript != "I go home." || repository.corrected != "I went home." || repository.shadowingID != "shadowing-job" {
		t.Fatalf("repository=%#v", repository)
	}
	if len(analyzer.inputs) != 1 || !reflect.DeepEqual(analyzer.inputs[0].Interests, []string{"travel"}) {
		t.Fatalf("analysis=%#v", analyzer.inputs)
	}
	if len(rewriter.inputs) != 1 || len(rewriter.inputs[0].Suggestions) != 1 {
		t.Fatalf("rewrite=%#v", rewriter.inputs)
	}
}

func TestProcessorResumesSuggestionStageWithoutAudioWork(t *testing.T) {
	repository := &processorRepository{
		found:              true,
		work:               ProcessingWork{UserID: "user-1", Stage: "suggestions", Transcript: "I go home.", EnglishLevel: "b1"},
		advanceSuggestions: true,
	}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Analyzer: &processorAnalyzer{}, Rewriter: &processorRewriter{},
		NewID:      func() string { return "shadowing-job" },
		Transcribe: func(context.Context, string) (string, error) { return "", errors.New("must not transcribe") },
	})
	if err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"load", "interests", "suggestions", "complete"}
	if !reflect.DeepEqual(repository.steps, want) {
		t.Fatalf("steps=%#v", repository.steps)
	}
}

func TestProcessorUsesWholeInterviewAudioAndTimedAnswers(t *testing.T) {
	end := 1000
	repository := &interviewProcessorRepository{
		processorRepository: &processorRepository{
			found: true, advanceTranscript: true, advanceSuggestions: true,
			work: ProcessingWork{UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1"),
				InterviewSessionID: pointer("session-1"), DeclaredDuration: 2, PracticeType: "topic"},
		},
		turns: []InterviewTurn{{Sequence: 1, AskedAtMS: 0, EndedAtMS: &end}, {Sequence: 2, AskedAtMS: 1000}},
	}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 1900 * time.Millisecond, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("the whole interview should use one timed transcription")
			return "", nil
		},
		TranscribeTimed: func(context.Context, string) (TimedTranscript, error) {
			return TimedTranscript{Text: "First. Second.", Segments: []TimedSegment{
				{StartMS: 100, EndMS: 800, Text: " First."},
				{StartMS: 1100, EndMS: 1700, Text: " Second."},
			}}, nil
		},
		Analyzer: &processorAnalyzer{}, Rewriter: &processorRewriter{}, NewID: func() string { return "shadowing-job" },
	})
	if err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	if repository.verifySeconds != 2 || repository.verifyMS != 1900 || repository.transcript != "First. Second." ||
		repository.answers[1] != "First." || repository.answers[2] != "Second." {
		t.Fatalf("unexpected interview processing: %#v", repository)
	}
}

func TestProcessorRejectsAccountAudioBeyondServerMeasuredLimit(t *testing.T) {
	repository := &processorRepository{
		found: true,
		work:  ProcessingWork{UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1")},
	}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) {
			return accountMaxDuration + time.Millisecond, nil
		},
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("over-limit audio must not be transcribed")
			return "", nil
		},
	})
	err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{})
	if err == nil || !reflect.DeepEqual(repository.steps, []string{"load"}) {
		t.Fatalf("err=%v steps=%#v", err, repository.steps)
	}
}

func TestProcessorStopsWhenLeaseProtectedTransitionLosesRace(t *testing.T) {
	repository := &processorRepository{
		found:             true,
		work:              ProcessingWork{UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1")},
		advanceTranscript: false,
	}
	processor := NewProcessor(ProcessingDependencies{
		Repository:   repository,
		Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) {
			return time.Second, nil
		},
		Transcribe: func(context.Context, string) (string, error) { return "I went home.", nil },
		Analyzer:   &processorAnalyzer{}, Rewriter: &processorRewriter{}, NewID: func() string { return "unused" },
	})
	if err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"load", "duration", "interests", "transcript"}
	if !reflect.DeepEqual(repository.steps, want) {
		t.Fatalf("steps=%#v", repository.steps)
	}
}

func TestStoredSuggestionsOmitDerivedLearningReference(t *testing.T) {
	stored := withoutReferences([]Suggestion{{
		Wrong: "she go", Right: "she goes", Explanation: "Match subject and verb.",
		Category: CategoryVerbGrammar, Severity: SeverityMedium, RuleID: "subject-verb-agreement",
		LearningReference: ReferenceFor("subject-verb-agreement", CategoryVerbGrammar),
	}})
	if len(stored) != 1 || stored[0].LearningReference != nil || stored[0].RuleID != "subject-verb-agreement" {
		t.Fatalf("stored=%#v", stored)
	}
}

func pointer(value string) *string { return &value }
