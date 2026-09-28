package recording

import (
	"context"
	"strings"
	"testing"
)

func TestParsePreviewCorrectionsKeepsOnlyTwoHighConfidenceMaterialErrors(t *testing.T) {
	transcript := "Yesterday I go to work and she have a meeting. It was nice."
	input := `{"corrections":[
		{"wrong":"Yesterday I go","right":"Yesterday I went","explanation":"Use past tense.","category":"verb_grammar","severity":"major","confidence":0.99},
		{"wrong":"she have","right":"she has","explanation":"Match the subject.","category":"verb_grammar","severity":"medium","confidence":0.95},
		{"wrong":"It was nice","right":"It was pleasant","explanation":"A style alternative.","category":"naturalness","severity":"minor","confidence":0.99},
		{"wrong":"work","right":"the office","explanation":"Uncertain preference.","category":"vocabulary","severity":"major","confidence":0.70}
	]}`
	got := parsePreviewCorrections(input, transcript)
	if len(got) != 2 || got[0].Wrong != "Yesterday I go" || got[1].Wrong != "she have" {
		t.Fatalf("got=%#v", got)
	}
}

func TestPreviewCorrectionsUsesBoundedNonThinkingJSONRequest(t *testing.T) {
	provider := &previewProvider{response: `{"corrections":[]}`}
	service := NewAnalysisService(provider, AnalysisConfig{Concurrency: 1})
	dialogue := []InterviewDialogueTurn{{Sequence: 1, Question: "Where did you go?", Answer: "I went home."}}
	got, err := service.PreviewCorrections(context.Background(), "I went home.", dialogue)
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("requests=%d", len(provider.requests))
	}
	request := provider.requests[0]
	if !request.ForceJSON || !request.DisableThinking || request.Temperature != 0.05 || !strings.Contains(request.SystemPrompt, "at most two") {
		t.Fatalf("request=%#v", request)
	}
	if !strings.Contains(request.UserPrompt, `"question":"Where did you go?"`) ||
		!strings.Contains(request.SystemPrompt, "questions are context only") {
		t.Fatalf("dialogue context or learner-only boundary missing: %#v", request)
	}
}

type previewProvider struct {
	response string
	requests []AnalysisCompletionRequest
}

func (p *previewProvider) Complete(_ context.Context, request AnalysisCompletionRequest) (string, error) {
	p.requests = append(p.requests, request)
	return p.response, nil
}
