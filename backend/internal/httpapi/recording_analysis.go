package httpapi

import (
	"context"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
)

// generateRecordingSuggestions is a transport compatibility seam while the
// recording worker is moved into its own application module. All analysis
// policy and provider orchestration lives in internal/recording.
func (s *Server) generateRecordingSuggestions(
	ctx context.Context,
	recordingID string,
	transcript string,
	topic string,
	interests []string,
	practiceType string,
	photoObject *string,
	englishLevel string,
	logger logging.Logger,
) ([]suggestion, error) {
	return s.recordingAnalyzer.Analyze(ctx, recording.AnalysisInput{
		RecordingID:  recordingID,
		Transcript:   transcript,
		Topic:        topic,
		Interests:    interests,
		PracticeType: practiceType,
		PhotoObject:  photoObject,
		EnglishLevel: englishLevel,
	}, logger)
}
