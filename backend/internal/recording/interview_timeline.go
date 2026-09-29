package recording

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInterviewTranscriptNotReady = errors.New("interview answer transcript is not ready")

type ComposedInterviewTranscript struct {
	Text     string
	Answers  map[int]string
	Dialogue []InterviewDialogueTurn
}

// ComposeInterviewTranscript builds the canonical learner-only transcript
// from completed per-turn transcription. The continuous interview recording
// remains the duration and playback source; it is not transcribed again.
func ComposeInterviewTranscript(turns []InterviewTurn) (ComposedInterviewTranscript, error) {
	if len(turns) == 0 {
		return ComposedInterviewTranscript{}, fmt.Errorf("%w: interview has no turns", ErrInterviewTranscriptNotReady)
	}
	answers := make(map[int]string, len(turns))
	dialogue := make([]InterviewDialogueTurn, 0, len(turns))
	parts := make([]string, 0, len(turns))
	previousSequence := 0
	for _, turn := range turns {
		if !validInterviewTurnSequence(previousSequence, turn.Sequence) {
			return ComposedInterviewTranscript{}, fmt.Errorf(
				"%w: turn sequence %d is invalid after %d",
				ErrInterviewTranscriptNotReady,
				turn.Sequence,
				previousSequence,
			)
		}
		previousSequence = turn.Sequence
		if turn.TranscriptStatus != "ready" {
			return ComposedInterviewTranscript{}, fmt.Errorf("%w: turn %d status is %s", ErrInterviewTranscriptNotReady, turn.Sequence, strings.TrimSpace(turn.TranscriptStatus))
		}
		var source *string
		if turn.FinalText != nil {
			source = turn.FinalText
		} else {
			source = turn.Provisional
		}
		if source == nil {
			return ComposedInterviewTranscript{}, fmt.Errorf("%w: turn %d has no final text", ErrInterviewTranscriptNotReady, turn.Sequence)
		}
		rawAnswer := strings.Join(strings.Fields(strings.TrimSpace(*source)), " ")
		answer := NormalizeTranscript(rawAnswer)
		if answer == "" {
			return ComposedInterviewTranscript{}, fmt.Errorf("%w: turn %d is empty", ErrInterviewTranscriptNotReady, turn.Sequence)
		}
		if answer != rawAnswer {
			return ComposedInterviewTranscript{}, errors.New("interview answer transcript exceeds the supported length")
		}
		question := strings.TrimSpace(turn.Question)
		if question == "" {
			return ComposedInterviewTranscript{}, fmt.Errorf("%w: turn %d has no question", ErrInterviewTranscriptNotReady, turn.Sequence)
		}
		answers[turn.Sequence] = answer
		parts = append(parts, answer)
		dialogue = append(dialogue, InterviewDialogueTurn{Sequence: turn.Sequence, Question: question, Answer: answer})
	}
	joined := strings.Join(parts, " ")
	text := NormalizeTranscript(joined)
	if text == "" || text != joined {
		return ComposedInterviewTranscript{}, errors.New("interview transcript exceeds the supported length")
	}
	return ComposedInterviewTranscript{Text: text, Answers: answers, Dialogue: dialogue}, nil
}

func validInterviewTurnSequence(previous, current int) bool {
	return current > 0 && current > previous
}
