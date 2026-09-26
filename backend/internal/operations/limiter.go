package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
)

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	ResetAfter time.Duration
	RetryAfter time.Duration
}

type Limiter struct {
	db  *db.DB
	now func() time.Time
}

func NewLimiter(database *db.DB) *Limiter {
	return &Limiter{db: database, now: time.Now}
}

func (l *Limiter) Allow(ctx context.Context, scope string, subject string, limit Limit) (Decision, error) {
	if l == nil || l.db == nil {
		return Decision{}, errors.New("rate-limit store is not configured")
	}
	scope = strings.TrimSpace(scope)
	subject = strings.TrimSpace(subject)
	if scope == "" || subject == "" || limit.Requests <= 0 || limit.Window <= 0 {
		return Decision{}, errors.New("invalid rate-limit request")
	}
	now := l.now().UTC()
	windowStart := floorTime(now, limit.Window)
	resetAt := windowStart.Add(limit.Window)
	hash := subjectHash(scope, subject)
	var count int
	var storedStart time.Time
	err := l.db.QueryRow(ctx, `
		INSERT INTO api_rate_limits
		  (scope, subject_hash, window_started_at, request_count, expires_at, updated_at)
		VALUES ($1, $2, $3, 1, $4, $5)
		ON CONFLICT (scope, subject_hash) DO UPDATE SET
		  window_started_at = CASE
		    WHEN api_rate_limits.window_started_at < EXCLUDED.window_started_at THEN EXCLUDED.window_started_at
		    ELSE api_rate_limits.window_started_at
		  END,
		  request_count = CASE
		    WHEN api_rate_limits.window_started_at < EXCLUDED.window_started_at THEN 1
		    ELSE api_rate_limits.request_count + 1
		  END,
		  expires_at = EXCLUDED.expires_at,
		  updated_at = EXCLUDED.updated_at
		RETURNING request_count, window_started_at`, scope, hash, windowStart, resetAt.Add(24*time.Hour), now).Scan(&count, &storedStart)
	if err != nil {
		return Decision{}, err
	}
	resetAt = storedStart.Add(limit.Window)
	resetAfter := resetAt.Sub(now)
	if resetAfter < time.Second {
		resetAfter = time.Second
	}
	remaining := limit.Requests - count
	if remaining < 0 {
		remaining = 0
	}
	retryAfter := time.Duration(0)
	if count > limit.Requests {
		retryAfter = resetAfter
	}
	return Decision{
		Allowed: count <= limit.Requests, Limit: limit.Requests,
		Remaining: remaining, ResetAt: resetAt, ResetAfter: resetAfter, RetryAfter: retryAfter,
	}, nil
}

func PruneExpiredRateLimits(ctx context.Context, database *db.DB, now time.Time, limit int) (int64, error) {
	if database == nil || now.IsZero() || limit <= 0 {
		return 0, errors.New("rate-limit prune store, boundary and limit are required")
	}
	result, err := database.Exec(ctx, `
		WITH candidates AS (
		  SELECT scope, subject_hash
		  FROM api_rate_limits
		  WHERE expires_at < $1
		  ORDER BY expires_at ASC
		  FOR UPDATE SKIP LOCKED
		  LIMIT $2
		)
		DELETE FROM api_rate_limits AS counter
		USING candidates
		WHERE counter.scope = candidates.scope
		  AND counter.subject_hash = candidates.subject_hash`, now.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func floorTime(value time.Time, window time.Duration) time.Time {
	nanos := value.UnixNano()
	return time.Unix(0, nanos-(nanos%window.Nanoseconds())).UTC()
}

func subjectHash(scope string, subject string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + subject))
	return hex.EncodeToString(sum[:])
}
