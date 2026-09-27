package feed

import "time"

type Post struct {
	ID                string
	SourceRecordingID string
	Topic             string
	Duration          int
	Transcript        string
	PracticeType      string
	AudioDataURL      *string
	PhotoDataURL      *string
	PhotoObject       *string
	SourceTimestamp   time.Time
	CreatedAt         time.Time
	AuthorMaskedEmail string
	ReplyCount        int
	Reactions         ReactionSummary
}

type Reply struct {
	ID                string
	PostID            string
	Duration          int
	AudioDataURL      *string
	Timestamp         time.Time
	CreatedAt         time.Time
	AuthorMaskedEmail string
	Reactions         ReactionSummary
}

type Thread struct {
	Post    Post
	Replies []Reply
}

type CreateReplyInput struct {
	PostID       string
	UserID       string
	UserEmail    string
	IsSubscriber bool
	Duration     int
	AudioDataURL string
	Timestamp    string
}

type CreateReplyResult struct {
	Reply Reply
	Quota Quota
}

type Quota struct {
	IsSubscriber           bool `json:"isSubscriber"`
	WeeklyLimitSeconds     *int `json:"weeklyLimitSeconds"`
	WeeklyUsedSeconds      int  `json:"weeklyUsedSeconds"`
	WeeklyRemainingSeconds *int `json:"weeklyRemainingSeconds"`
	MaxSessionSeconds      int  `json:"maxSessionSeconds"`
}

type ReactionCounts struct {
	Like    int `json:"like"`
	Love    int `json:"love"`
	Fire    int `json:"fire"`
	Laugh   int `json:"laugh"`
	Support int `json:"support"`
}

type ReactionSummary struct {
	Counts          ReactionCounts `json:"counts"`
	CurrentReaction *string        `json:"currentReaction"`
}
