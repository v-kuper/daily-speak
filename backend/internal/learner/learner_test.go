package learner

import (
	"strings"
	"testing"
)

func TestEnglishLevelAndInterestNormalization(t *testing.T) {
	if got := NormalizeEnglishLevel(" C1 "); got != "c1" {
		t.Fatalf("level = %q", got)
	}
	if got := NormalizeEnglishLevel("unknown"); got != DefaultEnglishLevel {
		t.Fatalf("fallback level = %q", got)
	}
	interests := NormalizeInterests([]string{" Travel ", "travel", "Product   design"}, 10)
	if len(interests) != 2 || interests[0] != "Travel" || interests[1] != "Product design" {
		t.Fatalf("interests = %#v", interests)
	}
}

func TestEnglishQuestionPromptGuidanceKeepsBeginnerQuestionsSimple(t *testing.T) {
	guidance := EnglishQuestionPromptGuidance("A1")
	for _, required := range []string{"at most 10 words", "one simple idea", "Avoid idioms"} {
		if !strings.Contains(guidance, required) {
			t.Fatalf("A1 question guidance %q does not contain %q", guidance, required)
		}
	}
	if got := EnglishQuestionPromptGuidance("unknown"); !strings.Contains(got, "at most 18 words") {
		t.Fatalf("unknown level guidance = %q", got)
	}
}
