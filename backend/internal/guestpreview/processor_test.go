package guestpreview

import (
	"context"
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

type previewAnalyzer struct{ transcript string }

func (a *previewAnalyzer) PreviewCorrections(_ context.Context, transcript string) ([]recording.Suggestion, error) {
	a.transcript = transcript
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

func TestGuestInterviewUsesWholeAudioAndKeepsAnswerTimeline(t *testing.T) {
	end := 1000
	sessionID := "session-1"
	store := &interviewProcessorStore{
		processorStore: &processorStore{work: ProcessingWork{AudioAssetID: "asset-1", DeclaredDuration: 2,
			InterviewSessionID: &sessionID}, found: true, advance: true, active: true},
		turns: []recording.InterviewTurn{{Sequence: 1, AskedAtMS: 0, EndedAtMS: &end},
			{Sequence: 2, AskedAtMS: 1000}},
	}
	processor := NewProcessor(ProcessorDependencies{
		Store: store, Materializer: &processorMaterializer{},
		ProbeAudioDuration: func(context.Context, string) (time.Duration, error) { return 2 * time.Second, nil },
		Transcribe: func(context.Context, string) (string, error) {
			t.Fatal("timed full-file transcription should be used")
			return "", nil
		},
		TranscribeTimed: func(context.Context, string) (recording.TimedTranscript, error) {
			return recording.TimedTranscript{Text: "First. Second.", Segments: []recording.TimedSegment{
				{StartMS: 100, EndMS: 800, Text: " First."},
				{StartMS: 1100, EndMS: 1700, Text: " Second."},
			}}, nil
		},
		Analyzer: &previewAnalyzer{},
	})
	if err := processor.Process(context.Background(), Job{ID: "job-1", ResourceID: "preview-1"}); err != nil {
		t.Fatal(err)
	}
	if store.sealMS != 2000 || store.transcript != "First. Second." || store.answers[1] != "First." || store.answers[2] != "Second." {
		t.Fatalf("unexpected guest timeline: %#v", store)
	}
}
