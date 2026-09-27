package feed

import (
	"context"
	"errors"
	"testing"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/quota"
)

func TestCreateReplyAppliesQuotaAndRollsBackStoredAudioOnPersistenceFailure(t *testing.T) {
	remaining := 60
	repository := &repositoryStub{
		quota:          Quota{WeeklyRemainingSeconds: &remaining, MaxSessionSeconds: quota.SubscriberMaxSessionSeconds},
		createReplyErr: errors.New("database unavailable"),
	}
	audio := &audioStoreStub{}
	service := NewService(repository, audio, func() string { return "reply-id" })

	_, err := service.CreateReply(context.Background(), CreateReplyInput{
		PostID: "post-id", UserID: "user-id", UserEmail: "person@example.com",
		Duration: 30, AudioDataURL: "data:audio/webm;base64,YQ==",
	})
	if err == nil {
		t.Fatal("expected persistence error")
	}
	if !audio.saved || !audio.rolledBack {
		t.Fatalf("audio lifecycle: saved=%v rolledBack=%v", audio.saved, audio.rolledBack)
	}
}

func TestCreateReplyRejectsWorkBeforeSavingWhenQuotaIsExceeded(t *testing.T) {
	remaining := 10
	audio := &audioStoreStub{}
	service := NewService(&repositoryStub{quota: Quota{WeeklyRemainingSeconds: &remaining}}, audio, func() string { return "reply-id" })

	_, err := service.CreateReply(context.Background(), CreateReplyInput{
		PostID: "post-id", UserID: "user-id", Duration: 30,
		AudioDataURL: "data:audio/webm;base64,YQ==",
	})
	if !errors.Is(err, ErrFreeQuotaExceeded) {
		t.Fatalf("error = %v, want quota exceeded", err)
	}
	if audio.saved {
		t.Fatal("audio was saved before quota admission")
	}
}

func TestSetReactionNormalizesAtApplicationBoundary(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &audioStoreStub{}, func() string { return "id" })
	reaction := " LOVE "

	if _, err := service.SetReaction(context.Background(), PostReaction, "post-id", "user-id", &reaction); err != nil {
		t.Fatalf("set reaction: %v", err)
	}
	if repository.reaction == nil || *repository.reaction != "love" {
		t.Fatalf("persisted reaction = %#v", repository.reaction)
	}

	invalid := "unknown"
	if _, err := service.SetReaction(context.Background(), PostReaction, "post-id", "user-id", &invalid); !errors.Is(err, ErrInvalidReaction) {
		t.Fatalf("invalid reaction error = %v", err)
	}
}

type repositoryStub struct {
	quota          Quota
	createReplyErr error
	reaction       *string
}

func (repository *repositoryStub) ListPosts(context.Context, string) ([]Post, error) {
	return nil, nil
}
func (repository *repositoryStub) PublishRecording(context.Context, string, string, string) (Post, bool, error) {
	return Post{}, false, nil
}
func (repository *repositoryStub) GetThread(context.Context, string, string) (Thread, error) {
	return Thread{}, nil
}
func (repository *repositoryStub) CreateReply(_ context.Context, id string, postID string, _ string, duration int, audioURL string, _ string) (Reply, error) {
	if repository.createReplyErr != nil {
		return Reply{}, repository.createReplyErr
	}
	return Reply{ID: id, PostID: postID, Duration: duration, AudioDataURL: &audioURL}, nil
}
func (repository *repositoryStub) SetReaction(_ context.Context, _ ReactionTarget, _ string, _ string, reaction *string) (ReactionSummary, error) {
	repository.reaction = reaction
	return EmptyReactionSummary(), nil
}
func (repository *repositoryStub) GetQuota(context.Context, string, bool) (Quota, error) {
	return repository.quota, nil
}

type audioStoreStub struct {
	saved      bool
	rolledBack bool
}

func (store *audioStoreStub) Save(context.Context, string, string, *media.ParsedAudioDataURL) (string, func(), error) {
	store.saved = true
	return "/uploads/feed-replies/user-id/reply-id.webm", func() { store.rolledBack = true }, nil
}
