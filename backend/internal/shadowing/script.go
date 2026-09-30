package shadowing

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Script is the legacy experimental sample response. It remains readable so
// clients can schedule its one-time replacement with corrected learner audio.
// New shadowing never generates this payload.
type SampleAnswer struct {
	Sequence   int    `json:"sequence"`
	Question   string `json:"question"`
	AnswerText string `json:"answerText"`
}
type Script struct {
	EnglishLevel string         `json:"englishLevel"`
	Text         string         `json:"text"`
	Turns        []SampleAnswer `json:"turns"`
}

func DecodeScript(payload []byte) *Script {
	var script Script
	if json.Unmarshal(payload, &script) != nil || len(script.Turns) == 0 || strings.TrimSpace(script.Text) == "" {
		return nil
	}
	parts := []string{}
	for i, turn := range script.Turns {
		if turn.Sequence < 1 || strings.TrimSpace(turn.Question) == "" || strings.TrimSpace(turn.AnswerText) == "" || (i > 0 && turn.Sequence <= script.Turns[i-1].Sequence) {
			return nil
		}
		parts = append(parts, turn.Question, turn.AnswerText)
	}
	if script.Text != strings.Join(parts, " ") || utf8.RuneCountInString(script.Text) > 20000 {
		return nil
	}
	return &script
}
