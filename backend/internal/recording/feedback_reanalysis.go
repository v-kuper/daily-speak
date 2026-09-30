package recording

import (
	"context"
	"errors"
	"regexp"
)

var ErrFeedbackInvalid = errors.New("invalid feedback request")
var ErrFeedbackConflict = errors.New("recording feedback is not ready for reanalysis")
var feedbackRequestKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

type FeedbackReanalysisRepository interface {
	ScheduleFeedback(context.Context, string, string, string) (bool, error)
}
type FeedbackReanalysisService struct{ repository FeedbackReanalysisRepository }

func NewFeedbackReanalysisService(repository FeedbackReanalysisRepository) *FeedbackReanalysisService {
	return &FeedbackReanalysisService{repository}
}
func (s *FeedbackReanalysisService) Schedule(ctx context.Context, owner, id, key string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, ErrFeedbackUnavailable
	}
	if owner == "" || id == "" || !feedbackRequestKey.MatchString(key) {
		return false, ErrFeedbackInvalid
	}
	return s.repository.ScheduleFeedback(ctx, owner, id, key)
}
