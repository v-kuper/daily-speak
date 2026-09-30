package app

import (
	"os"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/recording"
)

func recordingAnalysisConfig() recording.AnalysisConfig {
	concurrency, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("AI_ANALYSIS_CONCURRENCY")))
	return recording.AnalysisConfig{Concurrency: concurrency}
}
