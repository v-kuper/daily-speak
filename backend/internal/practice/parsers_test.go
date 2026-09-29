package practice

import "testing"

func TestParseDailyQuestionsRejectsNearDuplicateOpeningQuestions(t *testing.T) {
	_, ok := parseQuestions(`{"questions":[
		"What do you like about cooking?",
		"What do you enjoy about cooking?",
		"How do you learn new recipes?"
	]}`, dailyQuestionsCount)
	if ok {
		t.Fatal("near-duplicate opening questions should trigger regeneration")
	}
}

func TestParseTopicGuidancePreservesExpandedInterviewAndVocabulary(t *testing.T) {
	content := `{
		"questions":[
			"Question 1?","Question 2?","Question 3?","Question 4?","Question 5?",
			"Question 6?","Question 7?","Question 8?","Question 9?","Question 10?"
		],
		"words":[
			"phrase 1","phrase 2","phrase 3","phrase 4",
			"phrase 5","phrase 6","phrase 7","phrase 8"
		]
	}`

	guidance, ok := parseTopicGuidance(content)
	if !ok {
		t.Fatal("expected complete interview guidance to parse")
	}
	if len(guidance.Questions) != 10 {
		t.Fatalf("expected 10 ordered follow-up questions, got %d", len(guidance.Questions))
	}
	if guidance.Questions[0] != "Question 1?" || guidance.Questions[9] != "Question 10?" {
		t.Fatalf("expected question order to be preserved, got %#v", guidance.Questions)
	}
	if len(guidance.Words) != 8 {
		t.Fatalf("expected 8 useful words, got %d", len(guidance.Words))
	}
	if guidance.Words[0] != "phrase 1" || guidance.Words[7] != "phrase 8" {
		t.Fatalf("expected vocabulary order to be preserved, got %#v", guidance.Words)
	}
}

func TestParseTopicGuidanceRejectsIncompleteInterviewPayloads(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "nine follow-up questions",
			content: `{"questions":[
				"Q1?","Q2?","Q3?","Q4?","Q5?","Q6?","Q7?","Q8?","Q9?"
			],"words":[
				"w1","w2","w3","w4","w5","w6","w7","w8"
			]}`,
		},
		{
			name: "seven useful words",
			content: `{"questions":[
				"Q1?","Q2?","Q3?","Q4?","Q5?","Q6?","Q7?","Q8?","Q9?","Q10?"
			],"words":[
				"w1","w2","w3","w4","w5","w6","w7"
			]}`,
		},
		{
			name: "paraphrased follow-up",
			content: `{"questions":[
				"Q1?","Q2?","Q3?","Q4?","Q5?","Q6?","Q7?","Q8?",
				"What do you like about cooking?","What do you enjoy about cooking?"
			],"words":[
				"w1","w2","w3","w4","w5","w6","w7","w8"
			]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := parseTopicGuidance(tt.content); ok {
				t.Fatal("expected incomplete interview guidance to be rejected")
			}
		})
	}
}

func TestQuestionOverlapKeepsDifferentAnglesDistinct(t *testing.T) {
	if !anyQuestionOverlap([]string{"What do you enjoy about cooking?"}, []string{"What do you like about cooking?"}) {
		t.Fatal("expected a close paraphrase to overlap")
	}
	if anyQuestionOverlap([]string{"When did you start cooking?"}, []string{"Why did you start cooking?"}) {
		t.Fatal("questions about timing and motivation should be distinct")
	}
	if anyQuestionOverlap([]string{"How does health affect work?"}, []string{"How does work affect health?"}) {
		t.Fatal("questions with reversed cause and effect should be distinct")
	}
	if !anyQuestionOverlap([]string{"Tell me about your favorite trip?"}, []string{"What was your favorite trip?"}) {
		t.Fatal("questions with the same central phrase should overlap")
	}
	if !anyQuestionOverlap([]string{"Which food do you enjoy most?"}, []string{"What food do you enjoy most?"}) {
		t.Fatal("what and which versions of the same question should overlap")
	}
}

func TestParseTopicGuidancePlainTextKeepsNumberedWordsOutOfQuestions(t *testing.T) {
	content := `Questions:
1. Q1?
2. Q2?
3. Q3?
4. Q4?
5. Q5?
6. Q6?
7. Q7?
8. Q8?
9. Q9?
10. Q10?
Words:
1. first
2. second
3. third
4. fourth
5. fifth
6. sixth
7. seventh
8. eighth`

	guidance, ok := parseTopicGuidance(content)
	if !ok || len(guidance.Questions) != 10 || len(guidance.Words) != 8 ||
		guidance.Questions[9] != "Q10?" || guidance.Words[0] != "first" || guidance.Words[7] != "eighth" {
		t.Fatalf("unexpected plain-text guidance: %#v, ok=%t", guidance, ok)
	}
}
