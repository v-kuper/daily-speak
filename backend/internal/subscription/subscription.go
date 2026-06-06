package subscription

import (
	"context"
	"time"

	"daily-speaking-practice/backend/internal/db"
)

type State struct {
	IsSubscriber          bool    `json:"isSubscriber"`
	SubscriptionExpiresAt *string `json:"subscriptionExpiresAt"`
	SubscriptionCancelled bool    `json:"subscriptionCancelled"`
}

func GetState(ctx context.Context, database *db.DB, userID string) (State, error) {
	var active bool
	var expiresAt *time.Time
	var cancelled bool
	err := database.QueryRow(ctx, `
		SELECT
		  (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber_active,
		  subscription_expires_at,
		  subscription_cancelled
		FROM users
		WHERE id = $1
		LIMIT 1`, userID).Scan(&active, &expiresAt, &cancelled)
	if err != nil {
		return State{}, err
	}
	var expires *string
	if expiresAt != nil {
		value := expiresAt.UTC().Format(time.RFC3339Nano)
		expires = &value
	}
	return State{
		IsSubscriber:          active,
		SubscriptionExpiresAt: expires,
		SubscriptionCancelled: active && cancelled,
	}, nil
}
