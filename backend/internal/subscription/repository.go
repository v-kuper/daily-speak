package subscription

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
)

type SQLRepository struct{ db *db.DB }

func NewSQLRepository(database *db.DB) *SQLRepository {
	return &SQLRepository{db: database}
}

func (r *SQLRepository) State(ctx context.Context, userID string) (State, error) {
	if r == nil || r.db == nil {
		return State{}, errors.New("subscription database is not configured")
	}
	var active bool
	var expiresAt *time.Time
	var cancelled bool
	err := r.db.QueryRow(ctx, `
		SELECT
		  (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())),
		  subscription_expires_at,
		  subscription_cancelled
		FROM users
		WHERE id = $1
		LIMIT 1`, userID).Scan(&active, &expiresAt, &cancelled)
	return State{IsSubscriber: active, ExpiresAt: expiresAt, Cancelled: active && cancelled}, err
}

func (r *SQLRepository) Quota(ctx context.Context, userID string, subscriber bool) (quota.RecordingQuota, error) {
	if r == nil || r.db == nil {
		return quota.RecordingQuota{}, errors.New("subscription database is not configured")
	}
	return quota.GetRecordingQuota(ctx, r.db, userID, &subscriber)
}

func (r *SQLRepository) Activate(ctx context.Context, userID string) error {
	if r == nil || r.db == nil {
		return errors.New("subscription database is not configured")
	}
	_, err := r.db.Exec(ctx, `
		UPDATE users
		SET is_subscriber = TRUE,
		    subscription_cancelled = FALSE,
		    subscription_expires_at = CASE
		      WHEN subscription_expires_at IS NOT NULL AND subscription_expires_at > NOW()
		        THEN subscription_expires_at + INTERVAL '1 month'
		      ELSE NOW() + INTERVAL '1 month'
		    END
		WHERE id = $1`, userID)
	return err
}

func (r *SQLRepository) Cancel(ctx context.Context, userID string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("subscription database is not configured")
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_cancelled = TRUE,
		    subscription_expires_at = COALESCE(subscription_expires_at, NOW() + INTERVAL '1 month')
		WHERE id = $1
		  AND is_subscriber = TRUE
		  AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())`, userID)
	return err == nil && tag.RowsAffected() > 0, err
}
