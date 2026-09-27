package profile

import (
	"context"
	"errors"
	"testing"
)

type repositoryStub struct {
	level     *string
	saved     string
	interests []string
}

func (r *repositoryStub) EnglishLevel(context.Context, string) (*string, error) {
	return r.level, nil
}
func (r *repositoryStub) SaveEnglishLevel(_ context.Context, _ string, value string) error {
	r.saved = value
	return nil
}
func (r *repositoryStub) ReplaceInterests(_ context.Context, _ string, values []string) error {
	r.interests = values
	return nil
}
func (r *repositoryStub) Interests(context.Context, string) ([]string, error) {
	return r.interests, nil
}

func TestServiceOwnsProfileNormalization(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	level, err := service.EnglishLevel(context.Background(), "user")
	if err != nil || level != "b1" {
		t.Fatalf("level=%q err=%v", level, err)
	}
	if _, err := service.SaveEnglishLevel(context.Background(), "user", "invalid"); !errors.Is(err, ErrInvalidEnglishLevel) {
		t.Fatalf("err=%v", err)
	}
	level, err = service.SaveEnglishLevel(context.Background(), "user", " c1 ")
	if err != nil || level != "c1" || repository.saved != "c1" {
		t.Fatalf("level=%q saved=%q err=%v", level, repository.saved, err)
	}
	interests, err := service.ReplaceInterests(context.Background(), "user", []string{" travel ", "travel", "work"})
	if err != nil || len(interests) != 2 || interests[0] != "travel" || interests[1] != "work" {
		t.Fatalf("interests=%v err=%v", interests, err)
	}
}
