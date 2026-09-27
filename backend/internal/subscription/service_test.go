package subscription

import (
	"context"
	"errors"
	"testing"

	"daily-speaking-practice/backend/internal/quota"
)

type repositoryStub struct {
	state       State
	quota       quota.RecordingQuota
	activated   bool
	cancelled   bool
	cancelFound bool
}

func (r *repositoryStub) State(context.Context, string) (State, error) { return r.state, nil }
func (r *repositoryStub) Quota(context.Context, string, bool) (quota.RecordingQuota, error) {
	return r.quota, nil
}
func (r *repositoryStub) Activate(context.Context, string) error {
	r.activated = true
	return nil
}
func (r *repositoryStub) Cancel(context.Context, string) (bool, error) {
	r.cancelled = true
	return r.cancelFound, nil
}

func TestServiceComposesSubscriptionAndQuota(t *testing.T) {
	repository := &repositoryStub{
		state: State{IsSubscriber: true}, quota: quota.RecordingQuota{IsSubscriber: true},
	}
	service := NewService(repository)
	overview, err := service.Activate(context.Background(), "user")
	if err != nil || !repository.activated || !overview.State.IsSubscriber || !overview.Quota.IsSubscriber {
		t.Fatalf("overview=%+v repository=%+v err=%v", overview, repository, err)
	}
	_, err = service.Cancel(context.Background(), "user")
	if !errors.Is(err, ErrNoActiveSubscription) || !repository.cancelled {
		t.Fatalf("cancel err=%v repository=%+v", err, repository)
	}
	repository.cancelFound = true
	if _, err := service.Cancel(context.Background(), "user"); err != nil {
		t.Fatalf("cancel active: %v", err)
	}
}
