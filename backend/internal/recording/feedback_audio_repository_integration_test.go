package recording

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestSQLContextAudioKeepsLegacyCacheAndUsesCanonicalAnswer(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, url, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	fixture := newInterviewCompletionFixture(t, database)
	item := practiceFocus(t, "I go to Rome.", "go", "went", 1)
	item.Span.TurnSequence = 1
	payload, _ := json.Marshal(FocusedFeedback{Version: 1, Answers: []AnswerFeedback{{TurnSequence: 1, Items: []FeedbackFocus{item}}}})
	if _, err := database.Exec(ctx, `UPDATE recordings SET focused_feedback=$2::jsonb WHERE id=$1`, fixture.recordingID, string(payload)); err != nil {
		t.Fatal(err)
	}
	repository := NewSQLFeedbackAudioRepository(database)
	legacy := FeedbackAudioInput{OwnerID: fixture.userID, RecordingID: fixture.recordingID, FeedbackID: item.ID}
	contextID := feedbackPracticeContext("I go to Rome.", item).AudioFeedbackID
	input := legacy
	input.FeedbackID = contextID
	for _, lookup := range []FeedbackAudioInput{legacy, input} {
		focus, err := repository.FindFocus(ctx, lookup)
		if err != nil {
			t.Fatal(err)
		}
		expected := item.PracticeText
		if lookup.FeedbackID == contextID {
			expected = "I went to Rome."
		}
		if focus.PracticeText != expected {
			t.Fatalf("practice=%q expected=%q", focus.PracticeText, expected)
		}
		for i := 0; i < 2; i++ {
			if err := repository.ScheduleAudio(ctx, lookup, focus.PracticeText); err != nil {
				t.Fatal(err)
			}
		}
	}
	var rows, jobs int
	if err := database.QueryRow(ctx, `SELECT COUNT(*),COUNT(DISTINCT job_id) FROM recording_feedback_audio WHERE recording_id=$1`, fixture.recordingID).Scan(&rows, &jobs); err != nil || rows != 2 || jobs != 2 {
		t.Fatalf("rows=%d jobs=%d err=%v", rows, jobs, err)
	}
	if err := repository.ScheduleAudio(ctx, input, "Arbitrary replacement."); !errors.Is(err, ErrFeedbackUnavailable) {
		t.Fatalf("untrusted practice text: %v", err)
	}
	input.OwnerID = uuid.NewString()
	if _, err := repository.FindFocus(ctx, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM processing_jobs WHERE resource_id IN (SELECT id FROM recording_feedback_audio WHERE recording_id=$1)`, fixture.recordingID)
	})
}
