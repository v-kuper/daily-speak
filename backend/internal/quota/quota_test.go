package quota

import "testing"

func TestQuotaFormattingAndBounds(t *testing.T) {
	if got := nonNegative(-1); got != 0 {
		t.Fatalf("non-negative = %d", got)
	}
	if got := FormatSeconds(65); got != "1:05" {
		t.Fatalf("formatted seconds = %q", got)
	}
}
