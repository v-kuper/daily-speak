package subscription

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/quota"
)

var ErrNoActiveSubscription = errors.New("no active subscription")

type State struct {
	IsSubscriber bool
	ExpiresAt    *time.Time
	Cancelled    bool
}

type Overview struct {
	State State
	Quota quota.RecordingQuota
}

type Repository interface {
	State(context.Context, string) (State, error)
	Quota(context.Context, string, bool) (quota.RecordingQuota, error)
	Activate(context.Context, string) error
	Cancel(context.Context, string) (bool, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Get(ctx context.Context, userID string) (Overview, error) {
	state, err := s.repository.State(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	currentQuota, err := s.repository.Quota(ctx, userID, state.IsSubscriber)
	if err != nil {
		return Overview{}, err
	}
	return Overview{State: state, Quota: currentQuota}, nil
}

func (s *Service) Activate(ctx context.Context, userID string) (Overview, error) {
	if err := s.repository.Activate(ctx, userID); err != nil {
		return Overview{}, err
	}
	return s.Get(ctx, userID)
}

func (s *Service) Cancel(ctx context.Context, userID string) (Overview, error) {
	cancelled, err := s.repository.Cancel(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	if !cancelled {
		return Overview{}, ErrNoActiveSubscription
	}
	return s.Get(ctx, userID)
}
