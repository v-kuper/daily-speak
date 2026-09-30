package recording

import (
	"context"
	"daily-speaking-practice/backend/internal/media"
	"testing"
)

type audioTestRepository struct {
	state     FeedbackAudioState
	scheduled int
	kind      string
}

func (r *audioTestRepository) FindFocus(context.Context, FeedbackAudioInput) (FeedbackFocus, error) {
	return FeedbackFocus{Kind: r.kind, PracticeText: "I went home."}, nil
}
func (r *audioTestRepository) AudioState(context.Context, FeedbackAudioInput) (FeedbackAudioState, error) {
	return r.state, nil
}
func (r *audioTestRepository) ScheduleAudio(context.Context, FeedbackAudioInput, string) error {
	r.scheduled++
	r.state.Status = "processing"
	return nil
}

type audioTestSigner struct {
	calls int
	owner string
}

func (s *audioTestSigner) Download(_ context.Context, input media.DownloadInput) (media.Download, error) {
	s.calls++
	s.owner = input.OwnerPrincipalID
	return media.Download{Asset: media.Asset{ID: input.AssetID}}, nil
}
func TestFeedbackAudioGETNeverSchedulesAndPOSTIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := &audioTestRepository{state: FeedbackAudioState{Status: "not_requested"}}
	signer := &audioTestSigner{}
	service := NewFeedbackAudioService(repo, signer)
	input := FeedbackAudioInput{OwnerID: "owner"}
	if result, err := service.Audio(ctx, input, false); err != nil || result.Status != "not_requested" || repo.scheduled != 0 {
		t.Fatalf("GET=%+v err=%v", result, err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.Audio(ctx, input, true); err != nil {
			t.Fatal(err)
		}
	}
	if repo.scheduled != 1 {
		t.Fatalf("schedule count=%d", repo.scheduled)
	}
	repo.state = FeedbackAudioState{Status: "ready", AssetID: "audio"}
	for _, start := range []bool{false, true, false} {
		result, err := service.Audio(ctx, input, start)
		if err != nil || result.Download == nil {
			t.Fatal(err)
		}
	}
	if repo.scheduled != 1 || signer.calls != 3 || signer.owner != "owner" {
		t.Fatalf("repo=%+v signer=%+v", repo, signer)
	}
	repo.state.Status = "failed"
	_, _ = service.Audio(ctx, input, false)
	if repo.scheduled != 1 {
		t.Fatal("GET retried failed audio")
	}
	_, _ = service.Audio(ctx, input, true)
	if repo.scheduled != 2 {
		t.Fatal("POST failed retry missing")
	}
	repo.kind = "praise"
	if _, err := service.Audio(ctx, input, true); err != ErrFeedbackUnavailable {
		t.Fatalf("praise err=%v", err)
	}
}
