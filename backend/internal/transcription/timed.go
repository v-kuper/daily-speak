package transcription

import (
	"encoding/json"
	"math"
	"strings"
)

// TimedSegment keeps the provider's original spacing so the pieces can be
// rejoined exactly when assigning speech to interview questions.
type TimedSegment struct {
	StartMS int
	EndMS   int
	Text    string
}

type TimedResult struct {
	Text     string
	Segments []TimedSegment
}

func parseGroqTimedJSON(data []byte) (TimedResult, error) {
	var raw struct {
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
			Words []struct {
				Start float64 `json:"start"`
				End   float64 `json:"end"`
				Word  string  `json:"word"`
			} `json:"words"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TimedResult{}, err
	}
	text := normalizeTranscript(raw.Text)
	if text == "" {
		return TimedResult{}, Error{Message: "Groq returned an empty transcript.", Status: 422}
	}
	parts := make([]TimedSegment, 0, len(raw.Segments))
	var joined strings.Builder
	validTiming := true
	for _, segment := range raw.Segments {
		joined.WriteString(segment.Text)
		segmentParts := []TimedSegment{{StartMS: milliseconds(segment.Start), EndMS: milliseconds(segment.End), Text: segment.Text}}
		if len(segment.Words) > 0 {
			var wordsText strings.Builder
			words := make([]TimedSegment, 0, len(segment.Words))
			for _, word := range segment.Words {
				wordsText.WriteString(word.Word)
				words = append(words, TimedSegment{StartMS: milliseconds(word.Start), EndMS: milliseconds(word.End), Text: word.Word})
			}
			if normalizeTranscript(wordsText.String()) == normalizeTranscript(segment.Text) {
				segmentParts = words
			}
		}
		for _, part := range segmentParts {
			if part.StartMS < 0 || part.EndMS < part.StartMS {
				validTiming = false
			}
			parts = append(parts, part)
		}
	}
	if normalizeTranscript(joined.String()) != text || len([]rune(strings.Join(strings.Fields(raw.Text), " "))) > maxTranscriptLength {
		validTiming = false
	}
	if !validTiming {
		parts = nil
	}
	return TimedResult{Text: text, Segments: parts}, nil
}

func milliseconds(seconds float64) int {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt/1000) {
		return -1
	}
	return int(math.Round(seconds * 1000))
}
