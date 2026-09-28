package transcription

import "testing"

func TestParseCppTimedJSONPreservesTextAndOffsets(t *testing.T) {
	result, err := parseCppTimedJSON([]byte(`{
		"transcription":[
			{"offsets":{"from":100,"to":700},"text":" Hello"},
			{"offsets":{"from":800,"to":1400},"text":" world."}
		]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello world." || len(result.Segments) != 2 ||
		result.Segments[1].StartMS != 800 || result.Segments[1].Text != " world." {
		t.Fatalf("unexpected timed result: %#v", result)
	}
}

func TestParseOpenAITimedJSONUsesWordTimesWhenTextAgrees(t *testing.T) {
	result, err := parseOpenAITimedJSON([]byte(`{
		"text":" Hello world.",
		"segments":[{"start":0.2,"end":1.2,"text":" Hello world.",
			"words":[{"start":0.2,"end":0.5,"word":" Hello"},
			         {"start":0.7,"end":1.2,"word":" world."}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello world." || len(result.Segments) != 2 ||
		result.Segments[0].EndMS != 500 || result.Segments[1].StartMS != 700 {
		t.Fatalf("unexpected word timing: %#v", result)
	}
}

func TestParseOpenAITimedJSONKeepsTextWithoutTrustworthyOffsets(t *testing.T) {
	result, err := parseOpenAITimedJSON([]byte(`{
		"text":" Hello world.",
		"segments":[{"start":2.0,"end":1.0,"text":" Hello world."}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Hello world." || len(result.Segments) != 0 {
		t.Fatalf("expected text-only fallback, got %#v", result)
	}
}
