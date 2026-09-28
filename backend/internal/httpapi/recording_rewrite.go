package httpapi

import (
	"context"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) generateNaturalTranscript(ctx context.Context, transcript string, suggestions []suggestion, englishLevel string, logger logging.Logger) (string, error) {
	result, err := s.recordingRewriter.Rewrite(ctx, recording.RewriteInput{
		Transcript:   transcript,
		Suggestions:  suggestions,
		EnglishLevel: englishLevel,
	}, logger)
	return result.CorrectedTranscript, err
}
