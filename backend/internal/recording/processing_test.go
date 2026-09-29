package recording

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/quota"
)

type processorRepository struct {
	work               ProcessingWork
	found              bool
	interests          []string
	steps              []string
	duration           int
	transcript         string
	suggestions        []Suggestion
	strengths          []Strength
	corrected          RewriteResult
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
func (r *interviewProcessorRepository) LoadInterviewTimeline(context.Context, string) ([]InterviewTurn, error) {
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
func (r *processorRepository) SaveAnalysis(_ context.Context, _ ProcessingJob, analysis AnalysisResult) (bool, error) {
	r.steps = append(r.steps, "suggestions")
	r.suggestions = analysis.Suggestions
	r.strengths = analysis.Strengths
	return r.advanceSuggestions, nil
}
func (r *processorRepository) CompleteRecording(_ context.Context, _ ProcessingJob, corrected RewriteResult, shadowingID string) (bool, error) {
	r.steps = append(r.steps, "complete")
	r.corrected = corrected
	r.shadowingID = shadowingID
	return true, nil
}

type processorAnalyzer struct{ inputs []AnalysisInput }

func (a *processorAnalyzer) Analyze(_ context.Context, input AnalysisInput, _ AnalysisLogger) (AnalysisResult, error) {
	a.inputs = append(a.inputs, input)
	return AnalysisResult{
		Suggestions: []Suggestion{{Wrong: "go", Right: "went", Explanation: "Use past tense."}},
		Strengths:   []Strength{{Excerpt: "I", Explanation: "Clear subject.", Category: CategorySentenceStructure, RuleID: "word-order"}},
	}, nil
}

type processorRewriter struct {
	inputs []RewriteInput
	result RewriteResult
}

func (r *processorRewriter) Rewrite(_ context.Context, input RewriteInput, _ AnalysisLogger) (RewriteResult, error) {
	r.inputs = append(r.inputs, input)
	if r.result.CorrectedTranscript != "" {
		return r.result, nil
	}
	return RewriteResult{CorrectedTranscript: "I went home."}, nil
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
	if repository.duration != 31 || repository.transcript != "I go home." || repository.corrected.CorrectedTranscript != "I went home." || repository.shadowingID != "shadowing-job" {
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

func TestProcessorResumesInterviewSuggestionsWithDialogueContext(t *testing.T) {
	answer := "I go yesterday."
	repository := &interviewProcessorRepository{
		processorRepository: &processorRepository{
			found: true, advanceSuggestions: true,
			work: ProcessingWork{UserID: "user-1", Stage: "suggestions", Transcript: answer,
				InterviewSessionID: pointer("session-1"), PracticeType: "topic"},
		},
		turns: []InterviewTurn{{
			Sequence: 1, Question: "Where did you go yesterday?", TranscriptStatus: "ready", FinalText: &answer,
		}},
	}
	analyzer := &processorAnalyzer{}
	rewriter := &processorRewriter{result: RewriteResult{
		CorrectedTranscript: "Where did you go yesterday? I went home yesterday.",
		CorrectedAnswers: []CorrectedInterviewAnswer{{
			Sequence: 1, CorrectedAnswerText: "I went home yesterday.",
		}},
	}}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Analyzer: analyzer, Rewriter: rewriter,
		NewID: func() string { return "shadowing-job" },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("suggestion resume must not transcribe audio")
			return "", nil
		},
	})
	if err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	wantSteps := []string{"load", "interests", "turns", "suggestions", "turns", "complete"}
	if !reflect.DeepEqual(repository.steps, wantSteps) {
		t.Fatalf("steps=%#v", repository.steps)
	}
	wantDialogue := []InterviewDialogueTurn{{Sequence: 1, Question: "Where did you go yesterday?", Answer: answer}}
	if len(analyzer.inputs) != 1 || !reflect.DeepEqual(analyzer.inputs[0].InterviewTurns, wantDialogue) {
		t.Fatalf("analysis=%#v", analyzer.inputs)
	}
	if len(rewriter.inputs) != 1 || !reflect.DeepEqual(rewriter.inputs[0].InterviewTurns, wantDialogue) ||
		repository.corrected.CorrectedTranscript != "Where did you go yesterday? I went home yesterday." {
		t.Fatalf("rewrite=%#v corrected=%#v", rewriter.inputs, repository.corrected)
	}
}

func TestProcessorComposesReadyInterviewTurnsWithoutRetranscribingAudio(t *testing.T) {
	first, second := "First answer.", "Second answer."
	repository := &interviewProcessorRepository{
		processorRepository: &processorRepository{
			found: true, advanceTranscript: true, advanceSuggestions: true,
			work: ProcessingWork{UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1"),
				InterviewSessionID: pointer("session-1"), DeclaredDuration: 2, PracticeType: "topic"},
		},
		turns: []InterviewTurn{
			{Sequence: 1, Question: "First question?", TranscriptStatus: "ready", FinalText: &first},
			{Sequence: 2, Question: "Skipped question?", Skipped: true},
			{Sequence: 3, Question: "Another skipped question?", Skipped: true},
			{Sequence: 4, Question: "Second question?", TranscriptStatus: "ready", Provisional: &second},
		},
	}
	analyzer := &processorAnalyzer{}
	rewriter := &processorRewriter{result: RewriteResult{
		CorrectedTranscript: "First question? First corrected answer. Second question? Second corrected answer.",
		CorrectedAnswers: []CorrectedInterviewAnswer{
			{Sequence: 1, CorrectedAnswerText: "First corrected answer."},
			{Sequence: 4, CorrectedAnswerText: "Second corrected answer."},
		},
	}}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 1900 * time.Millisecond, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("continuous interview audio must not be transcribed")
			return "", nil
		},
		Analyzer: analyzer, Rewriter: rewriter, NewID: func() string { return "shadowing-job" },
	})
	if err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{}); err != nil {
		t.Fatal(err)
	}
	if repository.verifySeconds != 2 || repository.verifyMS != 1900 || repository.transcript != "First answer. Second answer." ||
		repository.answers[1] != first || repository.answers[4] != second {
		t.Fatalf("unexpected interview processing: %#v", repository)
	}
	wantDialogue := []InterviewDialogueTurn{
		{Sequence: 1, Question: "First question?", Answer: first},
		{Sequence: 4, Question: "Second question?", Answer: second},
	}
	if len(analyzer.inputs) != 1 || !reflect.DeepEqual(analyzer.inputs[0].InterviewTurns, wantDialogue) {
		t.Fatalf("analysis=%#v", analyzer.inputs)
	}
	if len(rewriter.inputs) != 1 || !reflect.DeepEqual(rewriter.inputs[0].InterviewTurns, wantDialogue) {
		t.Fatalf("rewrite=%#v", rewriter.inputs)
	}
	if got := repository.corrected.CorrectedTranscript; got != "First question? First corrected answer. Second question? Second corrected answer." {
		t.Fatalf("corrected transcript=%q", got)
	}
	if !reflect.DeepEqual(repository.corrected.CorrectedAnswers, rewriter.result.CorrectedAnswers) {
		t.Fatalf("corrected answers=%#v", repository.corrected.CorrectedAnswers)
	}
}

func TestProcessorRetriesInterviewUntilEveryTurnTranscriptIsReady(t *testing.T) {
	repository := &interviewProcessorRepository{
		processorRepository: &processorRepository{
			found: true, advanceTranscript: true,
			work: ProcessingWork{UserID: "user-1", Stage: "transcribing", AudioAssetID: pointer("asset-1"),
				InterviewSessionID: pointer("session-1"), PracticeType: "topic"},
		},
		turns: []InterviewTurn{{Sequence: 1, Question: "What happened?", TranscriptStatus: "queued"}},
	}
	processor := NewProcessor(ProcessingDependencies{
		Repository: repository, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return time.Second, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("missing turn text must not trigger full-audio transcription")
			return "", nil
		},
	})
	err := processor.Process(context.Background(), ProcessingJob{ID: "job-1", ResourceID: "recording-1"}, discardAnalysisLogger{})
	if !errors.Is(err, ErrInterviewTranscriptNotReady) {
		t.Fatalf("err=%v", err)
	}
	want := []string{"load", "verify", "interests", "turns"}
	if !reflect.DeepEqual(repository.steps, want) {
		t.Fatalf("steps=%#v", repository.steps)
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
			return time.Duration(quota.AccountMaxSessionSeconds)*time.Second + time.Millisecond, nil
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
