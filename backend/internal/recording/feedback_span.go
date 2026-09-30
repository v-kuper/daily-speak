package recording

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FeedbackSpan uses half-open UTF-16 offsets, matching browser string slices.
// Interview offsets are relative to the learner answer at TurnSequence.
type FeedbackSpan struct {
	Start        int `json:"start"`
	End          int `json:"end"`
	TurnSequence int `json:"turnSequence,omitempty"`
}

func feedbackWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}

func phrasePositions(text, phrase string) []int {
	if phrase == "" {
		return nil
	}
	first, _ := utf8.DecodeRuneInString(phrase)
	last, _ := utf8.DecodeLastRuneInString(phrase)
	var positions []int
	for from := 0; from < len(text); {
		index := strings.Index(text[from:], phrase)
		if index < 0 {
			break
		}
		start := from + index
		end := start + len(phrase)
		left, right := true, true
		if start > 0 && feedbackWordRune(first) {
			r, _ := utf8.DecodeLastRuneInString(text[:start])
			left = !feedbackWordRune(r)
		}
		if end < len(text) && feedbackWordRune(last) {
			r, _ := utf8.DecodeRuneInString(text[end:])
			right = !feedbackWordRune(r)
		}
		if left && right {
			positions = append(positions, start)
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		from = start + size
	}
	return positions
}

func utf16Length(text string) int {
	length := 0
	for _, r := range text {
		length++
		if r > 0xffff {
			length++
		}
	}
	return length
}

func resolveFeedbackSpan(transcript, phrase string, turns []InterviewDialogueTurn, sequence, occurrence *int) (*FeedbackSpan, bool) {
	if occurrence != nil && *occurrence < 1 {
		return nil, false
	}
	texts := []InterviewDialogueTurn{{Answer: transcript}}
	if len(turns) > 0 {
		texts = turns
	}
	var matches []*FeedbackSpan
	for _, turn := range texts {
		if sequence != nil && turn.Sequence != *sequence {
			continue
		}
		positions := phrasePositions(turn.Answer, phrase)
		for index, start := range positions {
			if occurrence != nil && index+1 != *occurrence {
				continue
			}
			matches = append(matches, &FeedbackSpan{Start: utf16Length(turn.Answer[:start]), End: utf16Length(turn.Answer[:start+len(phrase)]), TurnSequence: turn.Sequence})
		}
	}
	// Unspecified locations are accepted only when the evidence is unambiguous.
	if len(matches) != 1 {
		return nil, false
	}
	return matches[0], true
}

func feedbackSpansOverlap(a, b *FeedbackSpan) bool {
	return a != nil && b != nil && a.TurnSequence == b.TurnSequence && a.Start < b.End && b.Start < a.End
}

func feedbackID(kind, text string, span *FeedbackSpan) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%v:%s", kind, span, text)))
	return fmt.Sprintf("%s-%x", kind, hash[:8])
}
