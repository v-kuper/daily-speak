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
	for index, turn := range turns {
		if turn.Sequence != index+1 {
			return ComposedInterviewTranscript{}, fmt.Errorf("%w: turn sequence %d is missing", ErrInterviewTranscriptNotReady, index+1)
		}
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
