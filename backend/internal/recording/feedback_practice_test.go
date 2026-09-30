package recording

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func practiceFocus(t *testing.T, text, fragment, correction string, occurrence int) FeedbackFocus {
	t.Helper()
	span, valid := resolveFeedbackSpan(text, fragment, nil, nil, &occurrence)
	if !valid {
		t.Fatal("invalid test quotation")
	}
	return FeedbackFocus{ID: "saved-focus", Kind: "blocker", OriginalFragment: fragment, CorrectedFragment: correction, Occurrence: occurrence, Span: span, PracticeText: "An unrelated example."}
}

func TestPracticeUsesOnlyTheSelectedCorrectionInTheLearnersSentence(t *testing.T) {
	for _, test := range []struct {
		name, answer, original, corrected, before, after string
		occurrence                                       int
	}{
		{"sentence", "First sentence. Yesterday I go to the office. Next sentence.", "go", "went", "Yesterday I go to the office.", "Yesterday I went to the office.", 1},
		{"other error preserved", "I go to work and make a report yesterday.", "go", "went", "I go to work and make a report yesterday.", "I went to work and make a report yesterday.", 1},
		{"second occurrence", "I go to work and go home.", "go", "went", "I go to work and go home.", "I go to work and went home.", 2},
		{"UTF16 and emoji", "😀 Yesterday I go home! I stayed there.", "go", "went", "😀 Yesterday I go home!", "😀 Yesterday I went home!", 1},
		{"code switching", "I want to, ну как это, apply for the job.", "ну как это", "let me think", "I want to, ну как это, apply for the job.", "I want to, let me think, apply for the job.", 1},
		{"abbreviation and decimal", "I help Dr. Smith for 3.5 hours. Next sentence.", "help", "helped", "I help Dr. Smith for 3.5 hours.", "I helped Dr. Smith for 3.5 hours.", 1},
		{"newline", "Earlier answer\nI go to work\nLater answer", "go", "went", "I go to work", "I went to work", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := practiceFocus(t, test.answer, test.original, test.corrected, test.occurrence)
			context := feedbackPracticeContext(test.answer, item)
			if context == nil || context.OriginalText != test.before || context.CorrectedText != test.after {
				t.Fatalf("context=%+v", context)
			}
			if context.AudioFeedbackID == item.ID || context.AudioFeedbackID != feedbackPracticeContext(test.answer, item).AudioFeedbackID {
				t.Fatal("context must have a stable, separate audio identity")
			}
			item.PracticeText = context.CorrectedText
			if feedbackPracticeContext(test.answer, item).AudioFeedbackID != item.ID {
				t.Fatal("identical existing audio must be reused")
			}
		})
	}
}

func TestPracticeBoundsUnpunctuatedAnswersAndRejectsStaleAnchors(t *testing.T) {
	text := strings.Repeat("earlier word ", 120) + "I go home " + strings.Repeat("later word ", 120)
	item := practiceFocus(t, text, "go", "went", 1)
	context := feedbackPracticeContext(text, item)
	if context == nil || utf8.RuneCountInString(context.OriginalText) > 800 || utf8.RuneCountInString(context.CorrectedText) > 800 || !strings.Contains(context.CorrectedText, "I went home") || !strings.HasPrefix(context.CorrectedText, "… ") || !strings.HasSuffix(context.CorrectedText, " …") {
		t.Fatalf("context=%+v", context)
	}
	for _, changed := range []string{strings.Replace(text, "I go home", "I GO home", 1), "I go home"} {
		if feedbackPracticeContext(changed, item) != nil {
			t.Fatal("stale quote or offset must not produce practice")
		}
	}
	item.Kind = "praise"
	if feedbackPracticeContext(text, item) != nil {
		t.Fatal("praise must not produce a correction")
	}
}

func TestSavedPracticeStaysWithinItsAnswerAndKeepsLegacyFeedback(t *testing.T) {
	text := "Yesterday I go to work."
	item := practiceFocus(t, text, "go", "went", 1)
	item.Span.TurnSequence = 7
	item.PracticeContext = &FeedbackPracticeContext{CorrectedText: "Untrusted stored context"}
	stored, _ := json.Marshal(FocusedFeedback{Version: 1, Answers: []AnswerFeedback{{TurnSequence: 7, Items: []FeedbackFocus{item}}}})
	record := Record{Transcript: "Question with go. Different answer.", FocusedFeedbackJSON: stored, InterviewTurns: []InterviewTurn{{Sequence: 7, AnswerText: text}}}
	feedback := record.FocusedFeedback()
	got := feedback.Answers[0].Items[0]
	if got.PracticeContext == nil || got.PracticeContext.CorrectedText != "Yesterday I went to work." || got.ID != item.ID || got.PracticeText != item.PracticeText {
		t.Fatalf("item=%+v", got)
	}
	if string(record.FocusedFeedbackJSON) != string(stored) || record.InterviewTurns[0].AnswerText != text {
		t.Fatal("reading practice must not mutate the saved feedback or answer")
	}
	missing := DecodeFocusedFeedbackForAnswers(stored, map[int]string{3: text})
	if missing.Answers[0].Items[0].PracticeContext != nil {
		t.Fatal("context must not come from a different turn")
	}
}
