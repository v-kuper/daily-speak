package recording

import "strings"

type TimedSegment struct {
	StartMS int
	EndMS   int
	Text    string
}

type TimedTranscript struct {
	Text     string
	Segments []TimedSegment
}

// FinalInterviewAnswersWithinDuration refuses final answer attribution when
// click times or Whisper offsets extend beyond the measured complete audio.
// A two-second margin allows recorder and audio-probe rounding differences.
func FinalInterviewAnswersWithinDuration(turns []InterviewTurn, transcript TimedTranscript, audioDurationMS int) map[int]string {
	if !validInterviewTiming(turns, audioDurationMS) {
		return nil
	}
	limit := audioDurationMS + 2000
	for _, segment := range transcript.Segments {
		if segment.EndMS > limit {
			return nil
		}
	}
	return FinalInterviewAnswers(turns, transcript)
}

func validInterviewTiming(turns []InterviewTurn, audioDurationMS int) bool {
	if audioDurationMS <= 0 || len(turns) == 0 {
		return false
	}
	limit := audioDurationMS + 2000
	for _, turn := range turns {
		if turn.AskedAtMS < 0 || turn.AskedAtMS > limit ||
			(turn.EndedAtMS != nil && (*turn.EndedAtMS < turn.AskedAtMS || *turn.EndedAtMS > limit)) {
			return false
		}
	}
	return true
}

// FinalInterviewAnswers assigns every timed piece to the nearest question
// interval using its midpoint. The answer text is final, but the boundary is
// always approximate: Whisper timing does not identify an exact click instant.
func FinalInterviewAnswers(turns []InterviewTurn, transcript TimedTranscript) map[int]string {
	if len(turns) == 0 || len(transcript.Segments) == 0 {
		return nil
	}
	var complete strings.Builder
	for _, part := range transcript.Segments {
		complete.WriteString(part.Text)
	}
	if NormalizeTranscript(complete.String()) != NormalizeTranscript(transcript.Text) {
		return nil
	}
	for index, turn := range turns {
		if turn.Sequence < 1 || turn.AskedAtMS < 0 ||
			(index > 0 && (turn.Sequence <= turns[index-1].Sequence || turn.AskedAtMS < turns[index-1].AskedAtMS)) {
			return nil
		}
	}
	assigned := make([]strings.Builder, len(turns))
	previousOwner := -1
	for _, part := range transcript.Segments {
		if part.StartMS < 0 || part.EndMS < part.StartMS {
			return nil
		}
		midpoint := part.StartMS + (part.EndMS-part.StartMS)/2
		owner, bestDistance := -1, int(^uint(0)>>1)
		for index, turn := range turns {
			end := int(^uint(0) >> 1)
			if turn.EndedAtMS != nil {
				end = *turn.EndedAtMS
			} else if index+1 < len(turns) {
				end = turns[index+1].AskedAtMS
			}
			if end < turn.AskedAtMS {
				return nil
			}
			distance := 0
			if midpoint < turn.AskedAtMS {
				distance = turn.AskedAtMS - midpoint
			} else if midpoint > end {
				distance = midpoint - end
			}
			// On an exact question boundary the later question owns the text.
			if distance <= bestDistance {
				owner, bestDistance = index, distance
			}
		}
		if owner < 0 || owner < previousOwner {
			return nil
		}
		previousOwner = owner
		assigned[owner].WriteString(part.Text)
	}
	answers := make(map[int]string, len(turns))
	var rejoined strings.Builder
	for index, turn := range turns {
		answers[turn.Sequence] = NormalizeTranscript(assigned[index].String())
		rejoined.WriteString(assigned[index].String())
	}
	if NormalizeTranscript(rejoined.String()) != NormalizeTranscript(transcript.Text) {
		return nil
	}
	return answers
}

// FinalInterviewAnswersFromProvisional uses completed answer transcriptions
// only when their ordered text exactly reconstructs the full-file result.
// Different model wording or an unfinished answer leaves the timeline
// unaligned, so the canonical full transcript remains the only final text.
func FinalInterviewAnswersFromProvisional(turns []InterviewTurn, fullText string, audioDurationMS int) map[int]string {
	if !validInterviewTiming(turns, audioDurationMS) || strings.TrimSpace(fullText) == "" {
		return nil
	}
	answers := make(map[int]string, len(turns))
	parts := make([]string, 0, len(turns))
	for index, turn := range turns {
		if turn.Sequence < 1 || turn.Provisional == nil ||
			(index > 0 && turn.Sequence <= turns[index-1].Sequence) {
			return nil
		}
		answer := strings.Join(strings.Fields(*turn.Provisional), " ")
		answers[turn.Sequence] = answer
		if answer != "" {
			parts = append(parts, answer)
		}
	}
	if strings.Join(parts, " ") != strings.Join(strings.Fields(fullText), " ") {
		return nil
	}
	return answers
}
