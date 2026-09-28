package quota

import (
	"context"
	"strconv"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	GuestMaxSessionSeconds   = 3 * 60
	AccountMaxSessionSeconds = 10 * 60
)

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func FormatSeconds(seconds int) string {
	seconds = nonNegative(seconds)
	minutes, rest := seconds/60, seconds%60
	if rest < 10 {
		return strconv.Itoa(minutes) + ":0" + strconv.Itoa(rest)
	}
	return strconv.Itoa(minutes) + ":" + strconv.Itoa(rest)
}

type RecordingQuota struct {
	IsSubscriber           bool `json:"isSubscriber"`
	WeeklyLimitSeconds     *int `json:"weeklyLimitSeconds"`
	WeeklyUsedSeconds      int  `json:"weeklyUsedSeconds"`
	WeeklyRemainingSeconds *int `json:"weeklyRemainingSeconds"`
	MaxSessionSeconds      int  `json:"maxSessionSeconds"`
}

// LockRecordingQuota returns the account recording policy and informational
// weekly usage while holding the user row lock used by recording creation.
func LockRecordingQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) (RecordingQuota, error) {
	var isSubscriber bool
	if err := tx.QueryRow(ctx, `
		SELECT is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > $2)
		FROM users
		WHERE id = $1
		FOR UPDATE`, userID, now).Scan(&isSubscriber); err != nil {
		return RecordingQuota{}, err
	}
	var usedSeconds int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(duration), 0)::int
		FROM recordings
		WHERE user_id = $1
		  AND created_at >= date_trunc('week', $2::timestamptz)
		  AND created_at < date_trunc('week', $2::timestamptz) + INTERVAL '1 week'`, userID, now).Scan(&usedSeconds); err != nil {
		return RecordingQuota{}, err
	}
	usedSeconds = nonNegative(usedSeconds)
	return RecordingQuota{
		IsSubscriber:           isSubscriber,
		WeeklyLimitSeconds:     nil,
		WeeklyUsedSeconds:      usedSeconds,
		WeeklyRemainingSeconds: nil,
		MaxSessionSeconds:      AccountMaxSessionSeconds,
	}, nil
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

	return RecordingQuota{
		IsSubscriber:           isSubscriber,
		WeeklyLimitSeconds:     nil,
		WeeklyUsedSeconds:      nonNegative(usedSeconds),
		WeeklyRemainingSeconds: nil,
		MaxSessionSeconds:      AccountMaxSessionSeconds,
	}, nil
}
