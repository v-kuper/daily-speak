package aiparse

import (
	"reflect"
	"testing"
)

func TestExtractJSONCandidatesRemovesThinkingAndMarkdown(t *testing.T) {
	content := "<think>private reasoning</think>\n```json\nSome text {\"value\":{\"nested\":true}}\n```"
	want := []string{
		`Some text {"value":{"nested":true}}`,
		`{"value":{"nested":true}}`,
	}
	if got := ExtractJSONCandidates(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}
