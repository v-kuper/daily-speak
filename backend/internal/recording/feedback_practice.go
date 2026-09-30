package recording

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FeedbackPracticeContext isolates one correction in the learner's own speech.
// Its audio identity is separate from the legacy model-generated example.
type FeedbackPracticeContext struct {
	OriginalText    string `json:"originalText"`
	CorrectedText   string `json:"correctedText"`
	AudioFeedbackID string `json:"audioFeedbackId"`
}

func (r Record) FocusedFeedback() *FocusedFeedback {
	answers := map[int]string{0: r.Transcript}
	for _, turn := range r.InterviewTurns {
		if !turn.Skipped {
			answers[turn.Sequence] = turn.AnswerText
		}
	}
	return DecodeFocusedFeedbackForAnswers(r.FocusedFeedbackJSON, answers)
}

// Resolve from canonical answers on every read, including older saved feedback.
// Never trust a stored context if its quotation no longer matches that answer.
func DecodeFocusedFeedbackForAnswers(data []byte, answers map[int]string) *FocusedFeedback {
	feedback := DecodeFocusedFeedback(data)
	if feedback == nil {
		return nil
	}
	for ai := range feedback.Answers {
		answer := &feedback.Answers[ai]
		for ii := range answer.Items {
			item := &answer.Items[ii]
			item.PracticeContext = nil
			if text, exists := answers[answer.TurnSequence]; exists && item.Span != nil && item.Span.TurnSequence == answer.TurnSequence {
				item.PracticeContext = feedbackPracticeContext(text, *item)
			}
		}
	}
	return feedback
}

func feedbackPracticeContext(text string, item FeedbackFocus) *FeedbackPracticeContext {
	if (item.Kind != "blocker" && item.Kind != "native_tip") || item.Span == nil || item.Occurrence < 1 || strings.TrimSpace(item.CorrectedFragment) == "" || item.CorrectedFragment == item.OriginalFragment {
		return nil
	}
	positions := phrasePositions(text, item.OriginalFragment)
	if item.Occurrence > len(positions) {
		return nil
	}
	byteStart := positions[item.Occurrence-1]
	byteEnd := byteStart + len(item.OriginalFragment)
	if item.Span.Start != utf16Length(text[:byteStart]) || item.Span.End != utf16Length(text[:byteEnd]) {
		return nil
	}
	chars := []rune(text)
	start := utf8.RuneCountInString(text[:byteStart])
	end := start + utf8.RuneCountInString(item.OriginalFragment)
	left, right := start, end
	for left > 0 && !practiceSentenceBoundary(chars, left-1) {
		left--
	}
	for right < len(chars) {
		boundary := practiceSentenceBoundary(chars, right)
		right++
		if boundary {
			// Keep closing quotation marks attached to the sentence.
			for right < len(chars) && strings.ContainsRune("\"'”’)", chars[right]) {
				right++
			}
			break
		}
	}
	sentenceLeft, sentenceRight := left, right
	// Bound unpunctuated answers without cutting the target or surrounding words.
	// Four reserved runes allow ellipses at both clipped edges.
	budget := min(796, 796-utf8.RuneCountInString(item.CorrectedFragment)+end-start)
	if budget < end-start {
		return nil
	}
	if right-left > budget {
		left = max(left, start-(budget-(end-start))/2)
		right = min(right, left+budget)
		left = max(sentenceLeft, right-budget)
		for left < start && left > 0 && !unicode.IsSpace(chars[left-1]) {
			left++
		}
		for right > end && right < len(chars) && !unicode.IsSpace(chars[right]) {
			right--
		}
	}
	prefix := string(chars[left:start])
	suffix := string(chars[end:right])
	original := strings.TrimSpace(prefix + item.OriginalFragment + suffix)
	corrected := strings.TrimSpace(prefix + item.CorrectedFragment + suffix)
	if left > sentenceLeft {
		original, corrected = "… "+original, "… "+corrected
	}
	if right < sentenceRight {
		original, corrected = original+" …", corrected+" …"
	}
	audioID := feedbackID("context-v1", item.ID+"\x00"+corrected, item.Span)
	if item.PracticeText == corrected {
		audioID = item.ID
	}
	return &FeedbackPracticeContext{
		OriginalText: original, CorrectedText: corrected,
		AudioFeedbackID: audioID,
	}
}

func practiceSentenceBoundary(chars []rune, index int) bool {
	r := chars[index]
	if r == '\n' || r == '\r' {
		return true
	}
	if !strings.ContainsRune(".!?", r) {
		return false
	}
	next := index + 1
	for next < len(chars) && strings.ContainsRune("\"'”’)", chars[next]) {
		next++
	}
	if next < len(chars) && !unicode.IsSpace(chars[next]) {
		return false
	}
	if r != '.' {
		return true
	}
	wordStart := index
	for wordStart > 0 && (unicode.IsLetter(chars[wordStart-1]) || chars[wordStart-1] == '.') {
		wordStart--
	}
	word := strings.ToLower(string(chars[wordStart:index]))
	switch word {
	case "mr", "mrs", "ms", "dr", "prof", "sr", "jr", "e.g", "i.e", "vs":
		return false
	}
	return index-wordStart != 1 || !unicode.IsUpper(chars[wordStart])
}
