package shadowing

import (
	"context"
	"testing"

	"daily-speaking-practice/backend/internal/storage"
)

type processorStore struct {
	work      Work
	found     bool
	asset     Asset
	completed bool
}

func (s *processorStore) LoadWork(context.Context, Job) (Work, bool, error) {
	return s.work, s.found, nil
}
func (s *processorStore) Complete(_ context.Context, _ Job, asset Asset) (bool, error) {
	s.asset = asset
	return s.completed, nil
}

type synthesizer struct{ text string }

func (s *synthesizer) Synthesize(_ context.Context, text string) ([]byte, error) {
	s.text = text
	return []byte("ID3-audio"), nil
}

type processorLogger struct {
	event string
	meta  map[string]any
}

func (l *processorLogger) Info(event string, meta map[string]any) {
	l.event, l.meta = event, meta
}

func TestProcessorSynthesizesAndPersistsLocalAsset(t *testing.T) {
	root := t.TempDir()
	mediaStore, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := "Where did you go? I went home. What did you do next? I cooked dinner."
	repository := &processorStore{
		work:  Work{UserID: "user-1", CorrectedTranscript: dialogue},
		found: true, completed: true,
	}
	synth := &synthesizer{}
	logger := &processorLogger{}
	processor := NewProcessor(ProcessorDependencies{
		Store: repository, Synthesizer: synth, MediaStore: mediaStore,
		NewID: func() string { return "asset-1" },
	})
	job := Job{ID: "attempt-1", ResourceID: "recording-1", LeaseToken: "lease-1"}
	if err := processor.Process(context.Background(), job, logger); err != nil {
		t.Fatal(err)
	}
	if synth.text != dialogue || repository.asset.ID != "asset-1" ||
		repository.asset.StorageDriver != storage.BackendLocal || repository.asset.ObjectKey == "" {
		t.Fatalf("synth=%q asset=%#v", synth.text, repository.asset)
	}
	if logger.event != "shadowing.ready" || logger.meta["recordingId"] != "recording-1" {
		t.Fatalf("event=%q meta=%#v", logger.event, logger.meta)
	}
}

func TestProcessorSkipsStaleJob(t *testing.T) {
	processor := NewProcessor(ProcessorDependencies{Store: &processorStore{found: false}})
	if err := processor.Process(context.Background(), Job{ID: "stale"}, &processorLogger{}); err != nil {
		t.Fatal(err)
	}
}
