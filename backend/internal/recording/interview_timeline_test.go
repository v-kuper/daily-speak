package recording

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestComposeInterviewTranscriptUsesReadyTurnsInSequence(t *testing.T) {
	first, provisionalFirst := "I finally went home.", "I go home."
	second := " Then I made dinner. "
	result, err := ComposeInterviewTranscript([]InterviewTurn{
		{Sequence: 1, Question: "Where did you go?", TranscriptStatus: "ready", FinalText: &first, Provisional: &provisionalFirst},
		{Sequence: 2, Question: "What happened next?", TranscriptStatus: "ready", Provisional: &second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "I finally went home. Then I made dinner." {
		t.Fatalf("text=%q", result.Text)
	}
	if !reflect.DeepEqual(result.Answers, map[int]string{1: "I finally went home.", 2: "Then I made dinner."}) {
		t.Fatalf("answers=%#v", result.Answers)
	}
	wantDialogue := []InterviewDialogueTurn{
		{Sequence: 1, Question: "Where did you go?", Answer: "I finally went home."},
		{Sequence: 2, Question: "What happened next?", Answer: "Then I made dinner."},
	}
	if !reflect.DeepEqual(result.Dialogue, wantDialogue) {
		t.Fatalf("dialogue=%#v", result.Dialogue)
	}
}

func TestComposeInterviewTranscriptAllowsGapsFromSkippedTurns(t *testing.T) {
	first, fourth := "First answer.", "Fourth answer."
	result, err := ComposeInterviewTranscript([]InterviewTurn{
		{Sequence: 1, Question: "First question?", TranscriptStatus: "ready", FinalText: &first},
		{Sequence: 4, Question: "Fourth question?", TranscriptStatus: "ready", FinalText: &fourth},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "First answer. Fourth answer." ||
		!reflect.DeepEqual(result.Answers, map[int]string{1: first, 4: fourth}) {
		t.Fatalf("result=%#v", result)
	}
	wantDialogue := []InterviewDialogueTurn{
		{Sequence: 1, Question: "First question?", Answer: first},
		{Sequence: 4, Question: "Fourth question?", Answer: fourth},
	}
	if !reflect.DeepEqual(result.Dialogue, wantDialogue) {
		t.Fatalf("dialogue=%#v", result.Dialogue)
	}
}

func TestComposeInterviewTranscriptRejectsMissingAnswer(t *testing.T) {
	answer := "I stayed home."
	tests := []struct {
		name  string
		turns []InterviewTurn
	}{
		{"no turns", nil},
		{"invalid sequence", []InterviewTurn{{Sequence: 0, Question: "Why?", TranscriptStatus: "ready", FinalText: &answer}}},
		{"out of order", []InterviewTurn{
			{Sequence: 2, Question: "Why?", TranscriptStatus: "ready", FinalText: &answer},
			{Sequence: 1, Question: "What next?", TranscriptStatus: "ready", FinalText: &answer},
		}},
		{"queued", []InterviewTurn{{Sequence: 1, Question: "Why?", TranscriptStatus: "queued", FinalText: &answer}}},
		{"missing text", []InterviewTurn{{Sequence: 1, Question: "Why?", TranscriptStatus: "ready"}}},
		{"empty text", []InterviewTurn{{Sequence: 1, Question: "Why?", TranscriptStatus: "ready", FinalText: pointer("")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ComposeInterviewTranscript(test.turns)
			if !errors.Is(err, ErrInterviewTranscriptNotReady) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestComposeInterviewTranscriptRejectsOversizedCombinedText(t *testing.T) {
	answer := strings.Repeat("a", 20001)
	_, err := ComposeInterviewTranscript([]InterviewTurn{{
		Sequence: 1, Question: "Please continue.", TranscriptStatus: "ready", FinalText: &answer,
	}})
	if err == nil || errors.Is(err, ErrInterviewTranscriptNotReady) {
		t.Fatalf("err=%v", err)
	}
}

func TestInterviewTurnResolvesExactFinalAnswer(t *testing.T) {
	provisional, final := "I go", "I went"
	turn := InterviewTurn{Provisional: &provisional}
	turn.ResolveAnswer()
	if turn.AnswerText != "I go" || turn.AnswerSource != "provisional" {
		t.Fatalf("turn=%#v", turn)
	}
	turn.FinalText = &final
	turn.ResolveAnswer()
	if turn.AnswerText != "I went" || turn.AnswerSource != "final" || turn.AnswerAlignment != "" {
		t.Fatalf("turn=%#v", turn)
	}
}
