package app

import "testing"

func TestRecordingAnalysisConfigurationReadsOnlyAtComposition(t *testing.T) {
	t.Setenv("AI_ANALYSIS_CONCURRENCY", "2")
	if got := recordingAnalysisConfig().Concurrency; got != 2 {
		t.Fatalf("concurrency=%d", got)
	}
	t.Setenv("AI_ANALYSIS_CONCURRENCY", "bad")
	if got := recordingAnalysisConfig().Concurrency; got != 0 {
		t.Fatalf("concurrency=%d", got)
	}
}
