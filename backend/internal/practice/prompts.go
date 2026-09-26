package practice

import (
	"fmt"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

func dailyQuestionsPrompt(dateKey string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string) string {
	parts := []string{
		fmt.Sprintf("Generate exactly %d daily English speaking practice questions for %s.", dailyQuestionsCount, dateKey),
		"Audience: English learner level " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		"Questions must be short, practical, and suitable for a 1-3 minute spoken answer.",
		"Each question must be clearly tied to a concrete theme, not a generic life question.",
		"The theme should be explicit in the wording of each question.",
		fmt.Sprintf("All %d questions must be semantically different from each other.", dailyQuestionsCount),
		`Return only JSON with this exact shape: {"questions":["question 1","question 2","question 3"]}.`,
		"Do not add markdown, explanations, numbering, or extra keys.",
	}
	if len(interests) > 0 {
		parts = append(parts, "User interests (use as themes): "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Each question must map to one of these interests and mention that theme explicitly.")
		parts = append(parts, fmt.Sprintf("Use different interests across the %d questions when possible.", dailyQuestionsCount))
		parts = append(parts, "Do not generate off-topic or generic questions unrelated to the listed interests.")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Return a different set than earlier generations for the same date.")
	}
	if len(avoidQuestions) > 0 {
		parts = append(parts, "Do not reuse any of these previous questions: "+strings.Join(avoidQuestions, " | ")+".")
		parts = append(parts, "If a candidate is similar, replace it with a new angle.")
	}
	return strings.Join(parts, " ")
}

func topicGuidancePrompt(topic string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string, avoidWords []string) string {
	parts := []string{
		`Topic: "` + topic + `".`,
		"Generate guidance for an English speaking practice session for level " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		fmt.Sprintf("Return exactly %d follow-up questions that form one coherent interview after the topic question.", topicGuidanceQuestionsCnt),
		fmt.Sprintf("Return exactly %d useful words or short phrases connected to this topic.", topicGuidanceWordsCnt),
		"Useful words must match the learner level and stay understandable for that level.",
		"Order the interview from opening context through personal experience, concrete details, reasons, comparison, consequences, a hypothetical situation, practical advice, reflection, and a final conclusion.",
		"Each question must follow naturally from the topic while remaining answerable regardless of the learner's exact previous answer.",
		"Do not repeat the original topic question or assume that the learner gave a particular answer.",
		"All follow-up questions must be distinct in angle and not paraphrases of each other.",
		"Useful words should be diverse, not near-duplicates.",
		`Return only JSON with exactly two keys: {"questions":["q1","q2","q3","q4","q5","q6","q7","q8","q9","q10","q11","q12","q13","q14","q15","q16","q17"],"words":["w1","w2","w3","w4","w5","w6","w7","w8","w9","w10","w11","w12","w13","w14","w15","w16"]}.`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Learner interests: "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Keep questions and useful words relevant to these interests when possible.")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Make a different set than previous outputs.")
	}
	if len(avoidQuestions) > 0 {
		parts = append(parts, "Do not reuse any of these previous follow-up questions: "+strings.Join(avoidQuestions, " | ")+".")
	}
	if len(avoidWords) > 0 {
		parts = append(parts, "Do not reuse any of these previous useful words/phrases: "+strings.Join(avoidWords, " | ")+".")
	}
	return strings.Join(parts, " ")
}

func studyWordsPrompt(englishLevel string, interests []string, refreshToken string, avoidWords []string) string {
	parts := []string{
		"Generate vocabulary for English speaking/reading study.",
		"Learner level: " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		"Return exactly 10 useful English words (single words or short 2-word terms).",
		"Then write one cohesive text (120-180 words) that naturally uses these words in context.",
		"The text must be clear and practical so learner understands usage context.",
		`Return only JSON with this exact shape: {"words":["w1","w2","w3","w4","w5","w6","w7","w8","w9","w10"],"text":"..."}`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Prefer topics connected to these interests: "+strings.Join(interests, ", ")+".")
	}
	if len(avoidWords) > 0 {
		parts = append(parts, "Do not reuse these words: "+strings.Join(avoidWords, ", ")+".")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Generate a different set than previous outputs.")
	}
	return strings.Join(parts, " ")
}
