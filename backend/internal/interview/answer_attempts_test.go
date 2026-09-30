package interview

import (
	"context"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/workqueue"
	"errors"
	"testing"
)

type attemptTestStore struct {
	work        AnswerAttemptWork
	failPublish bool
	published   int
}

func (s *attemptTestStore) LoadAttemptWork(context.Context, workqueue.Job) (AnswerAttemptWork, bool, error) {
	return s.work, true, nil
}
func (s *attemptTestStore) SaveAttemptTranscript(context.Context, workqueue.Job, string, int) error {
	panic("persisted transcript should be reused")
}
func (s *attemptTestStore) SaveAttemptFeedback(_ context.Context, _ workqueue.Job, f *recording.FocusedFeedback) error {
	s.work.Attempt.Feedback = f
	return nil
}
func (s *attemptTestStore) CompleteAttempt(context.Context, workqueue.Job, *recording.FocusedFeedback) error {
	if s.failPublish {
		s.failPublish = false
		return errors.New("publication failed")
	}
	s.published++
	return nil
}

type attemptTestAnalyzer struct {
	calls    int
	original string
}

func (a *attemptTestAnalyzer) Analyze(ctx context.Context, input recording.AnalysisInput, _ recording.AnalysisLogger) (recording.AnalysisResult, error) {
	a.calls++
	if input.Pipeline != recording.FocusedPipeline || len(input.InterviewTurns) != 1 || input.InterviewTurns[0].Sequence != 3 || input.InterviewTurns[0].Answer != "I went home." || len(input.ContextTurns) != 2 || input.ContextTurns[1].Answer != a.original {
		panic("analysis escaped the retaken answer")
	}
	f := &recording.FocusedFeedback{Version: 1, Answers: []recording.AnswerFeedback{{TurnSequence: 3, Items: []recording.FeedbackFocus{}}}}
	if err := input.FocusedCheckpoint.SaveFocused(ctx, f); err != nil {
		return recording.AnalysisResult{}, err
	}
	return recording.AnalysisResult{FocusedFeedback: f}, nil
}
func TestAttemptAnalyzesOnlyNewAnswerAndReusesCheckpointAfterFailedPublication(t *testing.T) {
	ctx := context.Background()
	store := &attemptTestStore{failPublish: true, work: AnswerAttemptWork{Attempt: AnswerAttempt{ID: "attempt", TurnSequence: 3, Transcript: "I went home."}, Original: AttemptOriginal{Question: "What did you do?", Dialogue: []recording.InterviewDialogueTurn{{Sequence: 1, Answer: "First original."}, {Sequence: 3, Answer: "I go home yesterday."}}}}}
	analyzer := &attemptTestAnalyzer{original: store.work.Original.Dialogue[1].Answer}
	processor := NewAnswerAttemptProcessor(store, nil, nil, analyzer, nil)
	if err := processor.Process(ctx, workqueue.Job{}); err == nil {
		t.Fatal("missing publication error")
	}
	if err := processor.Process(ctx, workqueue.Job{}); err != nil {
		t.Fatal(err)
	}
	if analyzer.calls != 1 || store.published != 1 || store.work.Original.Dialogue[1].Answer != "I go home yesterday." {
		t.Fatalf("calls=%d published=%d", analyzer.calls, store.published)
	}
}
