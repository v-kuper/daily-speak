package practice

import (
	"fmt"
	"strings"

	"daily-speaking-practice/backend/internal/learner"
)

func dailyQuestionsPrompt(dateKey string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string) string {
	parts := []string{
		fmt.Sprintf("Generate exactly %d opening questions for English speaking practice on %s.", dailyQuestionsCount, dateKey),
		"Use the date only to vary the selection, not as a source of facts or a required question theme.",
		"Audience: English learner level " + learner.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + learner.EnglishLevelPromptGuidance(englishLevel),
		"Question form: " + learner.EnglishQuestionPromptGuidance(englishLevel),
		"The selected CEFR level is a hard maximum. Do not use vocabulary or grammar from a higher level.",
		"Each question is the first question of a longer interview: introduce one recognizable theme broadly enough to leave several concrete aspects for follow-up questions.",
		"Use one short, open-ended question with one idea per theme, suitable for a spoken answer. Never combine two requests with 'and'. Make the theme clear from the wording without simply asking 'What do you think about [interest]?'.",
		"A learner must be able to answer even without direct experience of the activity. Do not assume they own something, have done something, or hold a particular opinion.",
		fmt.Sprintf("Make all %d questions meaningfully different, not near-paraphrases. Prefer different themes when possible.", dailyQuestionsCount),
		`Return only JSON with this exact shape: {"questions":["question 1","question 2","question 3"]}.`,
		"Do not add markdown, explanations, numbering, or extra keys.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Learner interests (choose themes from these): "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Use different interests when at least three clearly distinct ones are available; otherwise choose distinct aspects of the selected interests.")
		parts = append(parts, "Keep each theme recognizable, but do not force the exact interest label into the question or mix unrelated interests.")
	} else {
		parts = append(parts, "With no interests provided, choose three distinct everyday themes a learner can discuss without specialist knowledge.")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Return a different set than earlier generations for the same date.")
	}
	if len(avoidQuestions) > 0 {
		parts = append(parts, "Previous questions to avoid: "+strings.Join(avoidQuestions, " | ")+".")
		parts = append(parts, "Do not repeat or closely paraphrase them; choose a different theme or a genuinely new angle.")
	}
	return strings.Join(parts, " ")
}

func topicGuidancePrompt(topic string, refreshToken string, englishLevel string, interests []string, avoidQuestions []string, avoidWords []string) string {
	parts := []string{
		`Selected opening question: "` + topic + `".`,
		"Generate guidance for an English speaking practice session for level " + learner.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + learner.EnglishLevelPromptGuidance(englishLevel),
		"Question form: " + learner.EnglishQuestionPromptGuidance(englishLevel),
		"The selected CEFR level is a hard maximum for every follow-up question.",
		"The selected question is already the first interview question. Treat its subject as the sole topic; do not include it again in the questions array.",
		fmt.Sprintf("Return exactly %d follow-up questions that together explore this subject in depth.", topicGuidanceQuestionsCnt),
		"Before writing, identify distinct relevant facets of this subject. Progress naturally from accessible context to concrete experiences or preferences, examples, reasons, challenges, alternatives, and reflection where relevant; do not force an irrelevant stage.",
		"Each follow-up should ask one idea about one specific facet and invite a meaningful spoken answer. Never combine two requests with 'and'. Avoid generic questions that could fit any topic, near-paraphrases, and repeated angles.",
		"Every question must make sense independently of the learner's previous answer. Do not assume an experience, preference, possession, plan, or opinion; do not invent facts about the learner or the topic.",
		fmt.Sprintf("Return exactly %d practical English words or short 2-3 word phrases the learner could naturally use when answering these questions.", topicGuidanceWordsCnt),
		"Cover different facets of the selected topic with level-appropriate vocabulary. Avoid synonyms, near-duplicates, jargon, and words too generic to help discuss this topic.",
		`Return only JSON with exactly two keys: {"questions":["q1","q2","q3","q4","q5","q6","q7","q8","q9","q10"],"words":["w1","w2","w3","w4","w5","w6","w7","w8"]}.`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Other learner interests, for context only: "+strings.Join(interests, ", ")+".")
		parts = append(parts, "Use an interest only if it directly clarifies the selected subject; never switch the interview to another interest.")
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
		"Generate a vocabulary pack for English speaking and reading practice around one coherent, concrete theme.",
		"Learner level: " + learner.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + learner.EnglishLevelPromptGuidance(englishLevel),
		"Return exactly 10 useful English words or short 2-word terms the learner could use to discuss the chosen theme.",
		"Choose varied, practical vocabulary rather than near-synonyms, inflections of the same word, or generic filler words.",
		"Then write one cohesive text (120-180 words) on that theme that naturally uses all 10 terms in context.",
		"Make the text clear enough for the learner to infer how each term is used. Avoid unsupported factual claims.",
		`Return only JSON with this exact shape: {"words":["w1","w2","w3","w4","w5","w6","w7","w8","w9","w10"],"text":"..."}`,
		"No markdown, no extra keys, no explanations.",
	}
	if len(interests) > 0 {
		parts = append(parts, "Choose one theme from these learner interests instead of blending unrelated interests: "+strings.Join(interests, ", ")+".")
	}
	if len(avoidWords) > 0 {
		parts = append(parts, "Do not reuse these words: "+strings.Join(avoidWords, ", ")+".")
	}
	if refreshToken != "" {
		parts = append(parts, "Variation key: "+refreshToken+". Generate a different set than previous outputs.")
	}
	return strings.Join(parts, " ")
}
