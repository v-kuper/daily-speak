package profile

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/learner"
)

var ErrInvalidEnglishLevel = errors.New("English level is invalid.")

type Repository interface {
	EnglishLevel(context.Context, string) (*string, error)
	SaveEnglishLevel(context.Context, string, string) error
	ReplaceInterests(context.Context, string, []string) error
	Interests(context.Context, string) ([]string, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) EnglishLevel(ctx context.Context, userID string) (string, error) {
	value, err := s.repository.EnglishLevel(ctx, userID)
	if err != nil {
		return "", err
	}
	if value == nil {
		return learner.DefaultEnglishLevel, nil
	}
	return learner.NormalizeEnglishLevel(*value), nil
}

func (s *Service) SaveEnglishLevel(ctx context.Context, userID string, value string) (string, error) {
	normalized, ok := learner.ParseEnglishLevel(value)
	if !ok {
		return "", ErrInvalidEnglishLevel
	}
	if err := s.repository.SaveEnglishLevel(ctx, userID, normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

func (s *Service) ReplaceInterests(ctx context.Context, userID string, values []string) ([]string, error) {
	normalized := learner.NormalizeInterests(values, 10)
	if err := s.repository.ReplaceInterests(ctx, userID, normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (s *Service) Interests(ctx context.Context, userID string) ([]string, error) {
	return s.repository.Interests(ctx, userID)
}
