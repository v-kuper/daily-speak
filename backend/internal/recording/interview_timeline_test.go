package recording

import "testing"

func TestFinalInterviewAnswersAssignsEveryWordExactlyOnce(t *testing.T) {
	firstEnd, secondEnd := 1000, 2200
	turns := []InterviewTurn{
		{Sequence: 1, Question: "What happened?", AskedAtMS: 0, EndedAtMS: &firstEnd},
		{Sequence: 2, Question: "What happened next?", AskedAtMS: 1000, EndedAtMS: &secondEnd},
	}
	transcript := TimedTranscript{Text: "I left home. Then I returned.", Segments: []TimedSegment{
		{StartMS: 100, EndMS: 400, Text: " I left"},
		{StartMS: 500, EndMS: 800, Text: " home."},
		{StartMS: 1100, EndMS: 1450, Text: " Then I"},
		{StartMS: 1600, EndMS: 2000, Text: " returned."},
	}}
	answers := FinalInterviewAnswers(turns, transcript)
	if len(answers) != 2 || answers[1] != "I left home." || answers[2] != "Then I returned." {
		t.Fatalf("unexpected answers: %#v", answers)
	}
}

func TestFinalInterviewAnswersAssignsBoundarySpanningPieceByMidpoint(t *testing.T) {
	end := 1000
	turns := []InterviewTurn{
		{Sequence: 1, AskedAtMS: 0, EndedAtMS: &end},
		{Sequence: 2, AskedAtMS: 1000},
	}
	transcript := TimedTranscript{Text: "I continued.", Segments: []TimedSegment{
		{StartMS: 900, EndMS: 1100, Text: " I continued."},
	}}
	if answers := FinalInterviewAnswers(turns, transcript); answers[1] != "" || answers[2] != "I continued." {
		t.Fatalf("boundary-spanning piece should be assigned once to the later turn: %#v", answers)
	}
}

func TestInterviewTurnPrefersFinalAnswerAndFallsBackToProvisional(t *testing.T) {
	provisional, final := "I go", "I went"
	turn := InterviewTurn{Provisional: &provisional}
	turn.ResolveAnswer()
	if turn.AnswerText != "I go" || turn.AnswerSource != "provisional" {
		t.Fatalf("unexpected provisional answer: %#v", turn)
	}
	turn.FinalText = &final
	turn.ResolveAnswer()
	if turn.AnswerText != "I went" || turn.AnswerSource != "final" || turn.AnswerAlignment != "approximate" {
		t.Fatalf("unexpected final answer: %#v", turn)
	}
	empty := ""
	turn.FinalText = &empty
	turn.ResolveAnswer()
	if turn.AnswerText != "" || turn.AnswerSource != "none" || turn.AnswerAlignment != "" {
		t.Fatalf("confirmed silence must replace a stale provisional answer: %#v", turn)
	}
}

func TestFinalInterviewAnswersFromProvisionalRequiresExactOrderedFullText(t *testing.T) {
	first, silent, third := "I left home.", "", "Then I returned."
	turns := []InterviewTurn{
		{Sequence: 1, Provisional: &first},
		{Sequence: 2, Provisional: &silent},
		{Sequence: 3, Provisional: &third},
	}
	answers := FinalInterviewAnswersFromProvisional(turns, "I left home. Then I returned.", 5000)
	if len(answers) != 3 || answers[1] != first || answers[2] != "" || answers[3] != third {
		t.Fatalf("unexpected conservative mapping: %#v", answers)
	}
	if got := FinalInterviewAnswersFromProvisional(turns, "I left home, then I returned.", 5000); got != nil {
		t.Fatalf("different full-file wording must remain unaligned: %#v", got)
	}
	turns[1].Provisional = nil
	if got := FinalInterviewAnswersFromProvisional(turns, "I left home. Then I returned.", 5000); got != nil {
		t.Fatalf("unfinished answer must remain unaligned: %#v", got)
	}
}

func TestFinalInterviewAttributionRejectsClickTimesBeyondVerifiedAudio(t *testing.T) {
	end := 12000
	answer := "I travelled by train."
	turns := []InterviewTurn{{Sequence: 1, AskedAtMS: 0, EndedAtMS: &end, Provisional: &answer}}
	timed := TimedTranscript{Text: answer, Segments: []TimedSegment{{StartMS: 100, EndMS: 900, Text: answer}}}
	if got := FinalInterviewAnswersWithinDuration(turns, timed, 3000); got != nil {
		t.Fatalf("unverified click time attributed final text: %#v", got)
	}
	if got := FinalInterviewAnswersFromProvisional(turns, answer, 3000); got != nil {
		t.Fatalf("unverified click time attributed provisional text: %#v", got)
	}
}
