package httpapi

import "testing"

func TestParseTopicGuidancePreservesExpandedInterviewAndVocabulary(t *testing.T) {
	content := `{
		"questions":[
			"Question 1?","Question 2?","Question 3?","Question 4?","Question 5?","Question 6?",
			"Question 7?","Question 8?","Question 9?","Question 10?","Question 11?","Question 12?",
			"Question 13?","Question 14?","Question 15?","Question 16?","Question 17?"
		],
		"words":[
			"phrase 1","phrase 2","phrase 3","phrase 4","phrase 5","phrase 6","phrase 7","phrase 8",
			"phrase 9","phrase 10","phrase 11","phrase 12","phrase 13","phrase 14","phrase 15","phrase 16"
		]
	}`

	guidance, ok := parseTopicGuidance(content)
	if !ok {
		t.Fatal("expected complete interview guidance to parse")
	}
	if len(guidance.Questions) != 17 {
		t.Fatalf("expected 17 ordered follow-up questions, got %d", len(guidance.Questions))
	}
	if guidance.Questions[0] != "Question 1?" || guidance.Questions[16] != "Question 17?" {
		t.Fatalf("expected question order to be preserved, got %#v", guidance.Questions)
	}
	if len(guidance.Words) != 16 {
		t.Fatalf("expected 16 useful words, got %d", len(guidance.Words))
	}
	if guidance.Words[0] != "phrase 1" || guidance.Words[15] != "phrase 16" {
		t.Fatalf("expected vocabulary order to be preserved, got %#v", guidance.Words)
	}
}

func TestParseTopicGuidanceRejectsIncompleteInterviewPayloads(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "sixteen follow-up questions",
			content: `{"questions":[
				"Q1?","Q2?","Q3?","Q4?","Q5?","Q6?","Q7?","Q8?",
				"Q9?","Q10?","Q11?","Q12?","Q13?","Q14?","Q15?","Q16?"
			],"words":[
				"w1","w2","w3","w4","w5","w6","w7","w8",
				"w9","w10","w11","w12","w13","w14","w15","w16"
			]}`,
		},
		{
			name: "fifteen useful words",
			content: `{"questions":[
				"Q1?","Q2?","Q3?","Q4?","Q5?","Q6?","Q7?","Q8?","Q9?",
				"Q10?","Q11?","Q12?","Q13?","Q14?","Q15?","Q16?","Q17?"
			],"words":[
				"w1","w2","w3","w4","w5","w6","w7","w8",
				"w9","w10","w11","w12","w13","w14","w15"
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
