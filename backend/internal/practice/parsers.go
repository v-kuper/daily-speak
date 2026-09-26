package practice

import (
	"encoding/json"
	"regexp"
	"strings"

	"daily-speaking-practice/backend/internal/aiparse"
)

var (
	numberedLinePattern = regexp.MustCompile(`^\d+\s*[\)\.\-:]`)
	wordsLabelPattern   = regexp.MustCompile(`(?i)^words?\s*:`)
	wordSeparator       = regexp.MustCompile(`[,|]`)
)

func parseQuestions(content string, limit int) ([]string, bool) {
	for _, candidate := range aiparse.ExtractJSONCandidates(content) {
		var payload struct {
			Questions []string `json:"questions"`
		}
		if json.Unmarshal([]byte(candidate), &payload) == nil {
			questions := normalizeQuestions(payload.Questions, limit)
			if len(questions) == limit {
				return questions, true
			}
		}
	}
	questions := normalizeQuestions(nonEmptyLines(aiparse.NormalizeContent(content)), limit)
	return questions, len(questions) == limit
}

func parseTopicGuidance(content string) (TopicGuidanceResult, bool) {
	tryParse := func(candidate string) (TopicGuidanceResult, bool) {
		var payload struct {
			Questions []string `json:"questions"`
			Words     []string `json:"words"`
		}
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			return TopicGuidanceResult{}, false
		}
		questions := normalizeQuestions(payload.Questions, 0)
		words := normalizeWords(payload.Words)
		if len(questions) < topicGuidanceQuestionsCnt || len(words) < topicGuidanceWordsCnt {
			return TopicGuidanceResult{}, false
		}
		return TopicGuidanceResult{
			Questions: questions[:topicGuidanceQuestionsCnt],
			Words:     words[:topicGuidanceWordsCnt],
		}, true
	}

	if parsed, ok := tryParse(content); ok {
		return parsed, true
	}
	for _, candidate := range aiparse.ExtractJSONCandidates(content) {
		if parsed, ok := tryParse(candidate); ok {
			return parsed, true
		}
	}

	var questionLines []string
	var wordLines []string
	for _, line := range nonEmptyLines(aiparse.NormalizeContent(content)) {
		if strings.Contains(line, "?") || numberedLinePattern.MatchString(line) {
			questionLines = append(questionLines, line)
		} else {
			wordLines = append(wordLines, line)
		}
	}
	questions := normalizeQuestions(questionLines, 0)
	words := normalizeWords(wordLines)
	if len(questions) < topicGuidanceQuestionsCnt || len(words) < topicGuidanceWordsCnt {
		return TopicGuidanceResult{}, false
	}
	return TopicGuidanceResult{
		Questions: questions[:topicGuidanceQuestionsCnt],
		Words:     words[:topicGuidanceWordsCnt],
	}, true
}

func parseStudyPack(content string, avoidWords []string) (StudyPackResult, bool) {
	for _, candidate := range aiparse.ExtractJSONCandidates(content) {
		var payload struct {
			Words      []string `json:"words"`
			Vocabulary []string `json:"vocabulary"`
			Text       string   `json:"text"`
			Story      string   `json:"story"`
			Paragraph  string   `json:"paragraph"`
		}
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		wordsRaw := payload.Words
		if len(wordsRaw) == 0 {
			wordsRaw = payload.Vocabulary
		}
		text := normalizeText(firstNonEmpty(payload.Text, payload.Story, payload.Paragraph))
		words := normalizeWordList(wordsRaw)
		if validStudyPack(words, text, avoidWords) {
			return StudyPackResult{Words: words[:10], Text: text}, true
		}
	}

	lines := nonEmptyLines(aiparse.NormalizeContent(content))
	if len(lines) < 4 {
		return StudyPackResult{}, false
	}
	wordsLine := ""
	for _, line := range lines {
		if wordsLabelPattern.MatchString(line) || strings.Contains(line, ",") {
			wordsLine = line
			break
		}
	}
	var candidateWords []string
	if wordsLine != "" {
		cleaned := wordsLabelPattern.ReplaceAllString(wordsLine, "")
		candidateWords = wordSeparator.Split(cleaned, -1)
	} else {
		candidateWords = lines[:minInt(len(lines), 10)]
	}
	textLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != wordsLine {
			textLines = append(textLines, line)
		}
	}
	words := normalizeWordList(candidateWords)
	text := normalizeText(strings.Join(textLines, "\n"))
	if validStudyPack(words, text, avoidWords) {
		return StudyPackResult{Words: words[:10], Text: text}, true
	}
	return StudyPackResult{}, false
}
