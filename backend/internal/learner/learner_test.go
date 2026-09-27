package learner

import "testing"

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
