package quota

import (
	"context"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/domain"
)

type RecordingQuota struct {
	IsSubscriber           bool `json:"isSubscriber"`
	WeeklyLimitSeconds     *int `json:"weeklyLimitSeconds"`
	WeeklyUsedSeconds      int  `json:"weeklyUsedSeconds"`
	WeeklyRemainingSeconds *int `json:"weeklyRemainingSeconds"`
	MaxSessionSeconds      int  `json:"maxSessionSeconds"`
}

func GetRecordingQuota(ctx context.Context, database *db.DB, userID string, knownSubscriber *bool) (RecordingQuota, error) {
	isSubscriber := false
	if knownSubscriber != nil {
		isSubscriber = *knownSubscriber
	} else {
		if err := database.QueryRow(ctx, `
			SELECT (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber
			FROM users
			WHERE id = $1
			LIMIT 1`, userID).Scan(&isSubscriber); err != nil {
			return RecordingQuota{}, err
		}
	}

	usedSeconds := 0
	if err := database.QueryRow(ctx, `
		SELECT COALESCE(SUM(duration), 0)::int AS used_seconds
		FROM recordings
		WHERE user_id = $1
		  AND created_at >= date_trunc('week', NOW())
		  AND created_at < date_trunc('week', NOW()) + INTERVAL '1 week'`, userID).Scan(&usedSeconds); err != nil {
		return RecordingQuota{}, err
	}

	if isSubscriber {
		return RecordingQuota{
			IsSubscriber:           true,
			WeeklyLimitSeconds:     nil,
			WeeklyUsedSeconds:      domain.ToNonNegativeInt(usedSeconds),
			WeeklyRemainingSeconds: nil,
			MaxSessionSeconds:      domain.SubscriberMaxSessionSeconds,
		}, nil
	}

	limit := domain.FreeWeeklyLimitSeconds
	remaining := limit - domain.ToNonNegativeInt(usedSeconds)
	if remaining < 0 {
		remaining = 0
	}
	return RecordingQuota{
		IsSubscriber:           false,
		WeeklyLimitSeconds:     &limit,
		WeeklyUsedSeconds:      domain.ToNonNegativeInt(usedSeconds),
		WeeklyRemainingSeconds: &remaining,
		MaxSessionSeconds:      domain.SubscriberMaxSessionSeconds,
	}, nil
}
