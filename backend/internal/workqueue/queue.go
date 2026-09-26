package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	KindRecordingProcess    = "recording.process"
	KindShadowingSynthesize = "shadowing.synthesize"
	KindMediaDelete         = "media.delete"
)

var ErrLeaseLost = errors.New("processing job lease was lost")

type Job struct {
	ID           string
	Kind         string
	ResourceID   string
	Payload      json.RawMessage
	Attempts     int
	MaxAttempts  int
	LeaseToken   string
	LeaseOwner   string
	LeaseExpires time.Time
}

type NewJob struct {
	ID             string
	Kind           string
	ResourceID     string
	IdempotencyKey string
	Payload        any
	Priority       int
	MaxAttempts    int
	AvailableAt    time.Time
}

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func Enqueue(ctx context.Context, execer Execer, job NewJob) error {
	job.ID = strings.TrimSpace(job.ID)
	job.Kind = strings.TrimSpace(job.Kind)
	job.ResourceID = strings.TrimSpace(job.ResourceID)
	job.IdempotencyKey = strings.TrimSpace(job.IdempotencyKey)
	if job.ID == "" || job.Kind == "" || job.ResourceID == "" || job.IdempotencyKey == "" {
		return errors.New("processing job identity is required")
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if job.AvailableAt.IsZero() {
		job.AvailableAt = time.Now().UTC()
	}
	payload := `{}`
	if job.Payload != nil {
		encoded, err := json.Marshal(job.Payload)
		if err != nil {
			return fmt.Errorf("encode processing job payload: %w", err)
		}
		payload = string(encoded)
	}
	_, err := execer.Exec(ctx, `
		INSERT INTO processing_jobs
		  (id, kind, resource_id, idempotency_key, payload, priority, max_attempts, available_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		job.ID, job.Kind, job.ResourceID, job.IdempotencyKey, payload, job.Priority, job.MaxAttempts, job.AvailableAt)
	if err != nil {
		return fmt.Errorf("enqueue processing job: %w", err)
	}
	return nil
}

type Store struct {
	db *db.DB
}

func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) Claim(ctx context.Context, owner string, kinds []string, leaseDuration time.Duration) (Job, bool, error) {
	if s == nil || s.db == nil {
		return Job{}, false, errors.New("processing job store is not configured")
	}
	if len(kinds) == 0 {
		return Job{}, false, errors.New("at least one processing job kind is required")
	}
	if leaseDuration <= 0 {
		return Job{}, false, errors.New("processing job lease duration must be positive")
	}
	leaseToken := uuid.NewString()
	leaseMilliseconds := leaseDuration.Milliseconds()
	var job Job
	err := s.db.QueryRow(ctx, `
		WITH candidate AS (
		  SELECT id
		  FROM processing_jobs
		  WHERE kind = ANY($3::text[])
		    AND attempts < max_attempts
		    AND (
		      (state IN ('queued', 'retry_wait') AND available_at <= NOW())
		      OR (state = 'running' AND lease_expires_at <= NOW())
		    )
		  ORDER BY priority DESC, available_at ASC, created_at ASC
		  FOR UPDATE SKIP LOCKED
		  LIMIT 1
		)
		UPDATE processing_jobs AS job
		SET state = 'running',
		    attempts = job.attempts + 1,
		    lease_token = $1,
		    lease_owner = $2,
		    lease_expires_at = NOW() + ($4 * INTERVAL '1 millisecond'),
		    heartbeat_at = NOW(),
		    started_at = COALESCE(job.started_at, NOW()),
		    updated_at = NOW()
		FROM candidate
		WHERE job.id = candidate.id
		RETURNING job.id, job.kind, job.resource_id, job.payload, job.attempts,
		          job.max_attempts, job.lease_token, job.lease_owner, job.lease_expires_at`,
		leaseToken, owner, kinds, leaseMilliseconds).Scan(
		&job.ID, &job.Kind, &job.ResourceID, &job.Payload, &job.Attempts,
		&job.MaxAttempts, &job.LeaseToken, &job.LeaseOwner, &job.LeaseExpires,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim processing job: %w", err)
	}
	return job, true, nil
}

func (s *Store) Heartbeat(ctx context.Context, job Job, leaseDuration time.Duration) error {
	result, err := s.db.Exec(ctx, `
		UPDATE processing_jobs
		SET heartbeat_at = NOW(),
		    lease_expires_at = NOW() + ($3 * INTERVAL '1 millisecond'),
		    updated_at = NOW()
		WHERE id = $1 AND state = 'running' AND lease_token = $2`,
		job.ID, job.LeaseToken, leaseDuration.Milliseconds())
	if err != nil {
		return fmt.Errorf("heartbeat processing job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, job Job) error {
	result, err := s.db.Exec(ctx, `
		UPDATE processing_jobs
		SET state = 'succeeded', completed_at = NOW(), updated_at = NOW(),
		    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL,
		    heartbeat_at = NULL, last_error = NULL
		WHERE id = $1 AND state = 'running' AND lease_token = $2`, job.ID, job.LeaseToken)
	if err != nil {
		return fmt.Errorf("complete processing job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

type TerminalFailureFunc func(context.Context, pgx.Tx, Job, string) error

func (s *Store) Fail(ctx context.Context, job Job, cause error, retryDelay time.Duration, finalize TerminalFailureFunc) (bool, error) {
	message := "processing failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = truncate(cause.Error(), 1000)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin processing job failure: %w", err)
	}
	defer tx.Rollback(ctx)

	terminal := job.Attempts >= job.MaxAttempts
	state := "retry_wait"
	if terminal {
		state = "failed"
	}
	result, err := tx.Exec(ctx, `
		UPDATE processing_jobs
		SET state = $3,
		    available_at = CASE WHEN $3 = 'retry_wait'
		      THEN NOW() + ($4 * INTERVAL '1 millisecond') ELSE available_at END,
		    completed_at = CASE WHEN $3 = 'failed' THEN NOW() ELSE NULL END,
		    last_error = $5,
		    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL,
		    heartbeat_at = NULL, updated_at = NOW()
		WHERE id = $1 AND state = 'running' AND lease_token = $2`,
		job.ID, job.LeaseToken, state, retryDelay.Milliseconds(), message)
	if err != nil {
		return false, fmt.Errorf("fail processing job: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, ErrLeaseLost
	}
	if terminal && finalize != nil {
		if err := finalize(ctx, tx, job, message); err != nil {
			return false, fmt.Errorf("finalize processing job failure: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit processing job failure: %w", err)
	}
	return terminal, nil
}

func RetryDelay(attempt int, base time.Duration, maximum time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if maximum < base {
		maximum = base
	}
	if attempt < 1 {
		attempt = 1
	}
	factor := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * factor)
	if delay < 0 || delay > maximum {
		return maximum
	}
	return delay
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
