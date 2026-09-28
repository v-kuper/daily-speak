package guestpreview

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"daily-speaking-practice/backend/internal/recording"
)

type processorStore struct {
	work        ProcessingWork
	found       bool
	steps       []string
	transcript  string
	duration    int
	corrections []recording.Suggestion
	advance     bool
	active      bool
}

type interviewProcessorStore struct {
	*processorStore
	turns   []recording.InterviewTurn
	answers map[int]string
	sealMS  int
}

func (s *interviewProcessorStore) SealInterviewLastTurn(_ context.Context, _ Job, _ string, milliseconds int) error {
	s.steps = append(s.steps, "seal")
	s.sealMS = milliseconds
	return nil
}
func (s *interviewProcessorStore) LoadInterviewTurns(context.Context, string) ([]recording.InterviewTurn, error) {
	s.steps = append(s.steps, "turns")
	return s.turns, nil
}
func (s *interviewProcessorStore) SaveInterviewTranscript(_ context.Context, _ Job, transcript string, duration int, _ string, answers map[int]string) (bool, error) {
	s.steps = append(s.steps, "interview_transcript")
	s.transcript, s.duration, s.answers = transcript, duration, answers
	return s.advance, nil
}

func (s *processorStore) Claim(context.Context, Job) (ProcessingWork, bool, error) {
	s.steps = append(s.steps, "claim")
	return s.work, s.found, nil
}
func (s *processorStore) SaveTranscript(_ context.Context, _ Job, transcript string, duration int) (bool, error) {
	s.steps = append(s.steps, "transcript")
	s.transcript, s.duration = transcript, duration
	return s.advance, nil
}
func (s *processorStore) UpdateDuration(_ context.Context, _ Job, duration int) error {
	s.steps = append(s.steps, "duration")
	s.duration = duration
	return nil
}
func (s *processorStore) IsActive(context.Context, Job) (bool, error) {
	s.steps = append(s.steps, "active")
	return s.active, nil
}
func (s *processorStore) Complete(_ context.Context, _ Job, corrections []recording.Suggestion) error {
	s.steps = append(s.steps, "complete")
	s.corrections = corrections
	return nil
}

type processorMaterializer struct{ cleaned bool }

func (m *processorMaterializer) Materialize(context.Context, string) (string, func(), error) {
	return "/tmp/guest.webm", func() { m.cleaned = true }, nil
}

type previewAnalyzer struct {
	transcript string
	dialogue   []recording.InterviewDialogueTurn
}

func (a *previewAnalyzer) PreviewCorrections(_ context.Context, transcript string, dialogue []recording.InterviewDialogueTurn) ([]recording.Suggestion, error) {
	a.transcript, a.dialogue = transcript, dialogue
	return []recording.Suggestion{{Wrong: "go", Right: "went"}}, nil
}

func TestProcessorVerifiesTranscribesAndCompletesPreview(t *testing.T) {
	store := &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", DeclaredDuration: 30}, found: true, advance: true, active: true}
	materializer := &processorMaterializer{}
	analyzer := &previewAnalyzer{}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: materializer,
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 30500 * time.Millisecond, nil },
		Transcribe:         func(context.Context, string) (string, error) { return "  I   go home. ", nil },
		Analyzer:           analyzer,
	})
	if err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1", LeaseToken: "lease"}); err != nil {
		t.Fatal(err)
	}
	if !materializer.cleaned {
		t.Fatal("temporary media was not cleaned")
	}
	if store.transcript != "I go home." || store.duration != 31 || analyzer.transcript != "I go home." || len(store.corrections) != 1 {
		t.Fatalf("store=%#v analyzer=%#v", store, analyzer)
	}
	want := []string{"claim", "transcript", "active", "complete"}
	if !reflect.DeepEqual(store.steps, want) {
		t.Fatalf("steps=%#v", store.steps)
	}
}

func TestProcessorResumesPersistedTranscriptWithoutRetranscribing(t *testing.T) {
	store := &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", Transcript: "I went home.", DeclaredDuration: 20}, found: true, active: true}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 20 * time.Second, nil },
		Transcribe:         func(context.Context, string) (string, error) { t.Fatal("unexpected transcription"); return "", nil },
		Analyzer:           &previewAnalyzer{},
	})
	if err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"claim", "active", "complete"}
	if !reflect.DeepEqual(store.steps, want) {
		t.Fatalf("steps=%#v", store.steps)
	}
}

func TestProcessorRejectsGuestAudioBeyondMeasuredThreeMinuteLimit(t *testing.T) {
	store := &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", DeclaredDuration: 180}, found: true}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) {
			return 180*time.Second + time.Millisecond, nil
		},
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("over-limit guest audio must not be transcribed")
			return "", nil
		},
	})
	if err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1"}); err == nil {
		t.Fatal("guest audio beyond three minutes was accepted")
	}
}

func TestGuestInterviewComposesReadyTurnsWithoutRetranscribingAudio(t *testing.T) {
	sessionID := "session-1"
	first, second := "First answer.", "Second answer."
	store := &interviewProcessorStore{
		processorStore: &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", DeclaredDuration: 2,
			InterviewSessionID: &sessionID}, found: true, advance: true, active: true},
		turns: []recording.InterviewTurn{
			{Sequence: 1, Question: "First question?", TranscriptStatus: "ready", FinalText: &first},
			{Sequence: 2, Question: "Second question?", TranscriptStatus: "ready", Provisional: &second},
		},
	}
	analyzer := &previewAnalyzer{}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 2 * time.Second, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("continuous interview audio must not be transcribed")
			return "", nil
		},
		Analyzer: analyzer,
	})
	if err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1"}); err != nil {
		t.Fatal(err)
	}
	if store.sealMS != 2000 || store.transcript != "First answer. Second answer." || store.answers[1] != first || store.answers[2] != second {
		t.Fatalf("unexpected guest timeline: %#v", store)
	}
	wantDialogue := []recording.InterviewDialogueTurn{
		{Sequence: 1, Question: "First question?", Answer: first},
		{Sequence: 2, Question: "Second question?", Answer: second},
	}
	if analyzer.transcript != store.transcript || !reflect.DeepEqual(analyzer.dialogue, wantDialogue) {
		t.Fatalf("analyzer=%#v", analyzer)
	}
}

func TestGuestInterviewRetriesUntilTurnTranscriptIsReady(t *testing.T) {
	sessionID := "session-1"
	store := &interviewProcessorStore{
		processorStore: &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", DeclaredDuration: 1,
			InterviewSessionID: &sessionID}, found: true, advance: true},
		turns: []recording.InterviewTurn{{Sequence: 1, Question: "What happened?", TranscriptStatus: "queued"}},
	}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return time.Second, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("missing turn text must not trigger full-audio transcription")
			return "", nil
		},
	})
	err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1"})
	if !errors.Is(err, recording.ErrInterviewTranscriptNotReady) {
		t.Fatalf("err=%v", err)
	}
	want := []string{"claim", "seal", "turns"}
	if !reflect.DeepEqual(store.steps, want) {
		t.Fatalf("steps=%#v", store.steps)
	}
}
