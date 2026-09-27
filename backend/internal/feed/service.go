package feed

import (
	"context"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
)

var (
	ErrNotFound           = errors.New("feed resource not found")
	ErrInvalidRequest     = errors.New("feed request is invalid")
	ErrInvalidReaction    = errors.New("feed reaction is invalid")
	ErrInvalidReply       = errors.New("feed reply is invalid")
	ErrVoiceReplyRequired = errors.New("feed voice reply is required")
	ErrFreeQuotaExceeded  = errors.New("feed free quota exceeded")
	ErrSubscriberTooLong  = errors.New("feed subscriber reply is too long")
	ErrPersistence        = errors.New("feed persistence failed")
	ErrReplyStorage       = errors.New("feed reply storage failed")
)

type Repository interface {
	ListPosts(context.Context, string) ([]Post, error)
	PublishRecording(context.Context, string, string, string) (Post, bool, error)
	GetThread(context.Context, string, string) (Thread, error)
	CreateReply(context.Context, string, string, string, int, string, string) (Reply, error)
	SetReaction(context.Context, ReactionTarget, string, string, *string) (ReactionSummary, error)
	GetQuota(context.Context, string, bool) (Quota, error)
}

type ReplyAudioStore interface {
	Save(context.Context, string, string, *domain.ParsedAudioDataURL) (string, func(), error)
}

type ReactionTarget string

const (
	PostReaction  ReactionTarget = "post"
	ReplyReaction ReactionTarget = "reply"
)

type Service struct {
	repository Repository
	audioStore ReplyAudioStore
	newID      func() string
}

func NewService(repository Repository, audioStore ReplyAudioStore, newID func() string) *Service {
	return &Service{repository: repository, audioStore: audioStore, newID: newID}
}

func (service *Service) ListPosts(ctx context.Context, viewerID string) ([]Post, error) {
	if !service.available() || strings.TrimSpace(viewerID) == "" {
		return nil, ErrInvalidRequest
	}
	return service.repository.ListPosts(ctx, strings.TrimSpace(viewerID))
}

func (service *Service) PublishRecording(ctx context.Context, userID string, recordingID string) (Post, bool, error) {
	if !service.available() || strings.TrimSpace(userID) == "" || strings.TrimSpace(recordingID) == "" {
		return Post{}, false, ErrInvalidRequest
	}
	return service.repository.PublishRecording(ctx, strings.TrimSpace(userID), strings.TrimSpace(recordingID), service.newID())
}

func (service *Service) GetThread(ctx context.Context, viewerID string, postID string) (Thread, error) {
	if !service.available() || strings.TrimSpace(viewerID) == "" || strings.TrimSpace(postID) == "" {
		return Thread{}, ErrInvalidRequest
	}
	return service.repository.GetThread(ctx, strings.TrimSpace(postID), strings.TrimSpace(viewerID))
}

func (service *Service) CreateReply(ctx context.Context, input CreateReplyInput) (CreateReplyResult, error) {
	if !service.available() || service.audioStore == nil || strings.TrimSpace(input.PostID) == "" || strings.TrimSpace(input.UserID) == "" {
		return CreateReplyResult{}, ErrInvalidRequest
	}
	if input.Duration <= 0 {
		return CreateReplyResult{}, ErrInvalidReply
	}
	audio := domain.ParseIncomingAudioDataURL(input.AudioDataURL)
	if audio == nil {
		return CreateReplyResult{}, ErrVoiceReplyRequired
	}
	quota, err := service.repository.GetQuota(ctx, input.UserID, input.IsSubscriber)
	if err != nil {
		return CreateReplyResult{}, err
	}
	if err := validateQuota(quota, input.Duration); err != nil {
		return CreateReplyResult{}, err
	}
	replyID := service.newID()
	publicURL, rollback, err := service.audioStore.Save(ctx, input.UserID, replyID, audio)
	if err != nil {
		return CreateReplyResult{}, ErrReplyStorage
	}
	if rollback == nil {
		rollback = func() {}
	}
	reply, err := service.repository.CreateReply(ctx, replyID, strings.TrimSpace(input.PostID), input.UserID, input.Duration, publicURL, input.Timestamp)
	if err != nil {
		rollback()
		return CreateReplyResult{}, err
	}
	reply.AuthorMaskedEmail = maskEmail(input.UserEmail)
	reply.Reactions = EmptyReactionSummary()
	return CreateReplyResult{Reply: reply, Quota: quotaAfterSave(quota, input.Duration)}, nil
}

func (service *Service) SetReaction(ctx context.Context, target ReactionTarget, targetID string, userID string, reaction *string) (ReactionSummary, error) {
	if !service.available() || strings.TrimSpace(targetID) == "" || strings.TrimSpace(userID) == "" || (target != PostReaction && target != ReplyReaction) {
		return ReactionSummary{}, ErrInvalidRequest
	}
	var normalized *string
	if reaction != nil && strings.TrimSpace(*reaction) != "" {
		value, valid := NormalizeReaction(*reaction)
		if !valid {
			return ReactionSummary{}, ErrInvalidReaction
		}
		normalized = &value
	}
	return service.repository.SetReaction(ctx, target, strings.TrimSpace(targetID), strings.TrimSpace(userID), normalized)
}

func (service *Service) available() bool {
	return service != nil && service.repository != nil && service.newID != nil
}

func validateQuota(quota Quota, duration int) error {
	if quota.IsSubscriber {
		if duration > domain.SubscriberMaxSessionSeconds {
			return ErrSubscriberTooLong
		}
		return nil
	}
	remaining := 0
	if quota.WeeklyRemainingSeconds != nil {
		remaining = *quota.WeeklyRemainingSeconds
	}
	if duration > remaining {
		return ErrFreeQuotaExceeded
	}
	return nil
}

func quotaAfterSave(before Quota, duration int) Quota {
	after := before
	savedSeconds := nonNegative(duration)
	after.WeeklyUsedSeconds = nonNegative(before.WeeklyUsedSeconds) + savedSeconds
	if before.WeeklyRemainingSeconds != nil {
		remaining := nonNegative(*before.WeeklyRemainingSeconds) - savedSeconds
		if remaining < 0 {
			remaining = 0
		}
		after.WeeklyRemainingSeconds = &remaining
	}
	return after
}
