CREATE TABLE api_rate_limits (
  scope TEXT NOT NULL,
  subject_hash TEXT NOT NULL,
  window_started_at TIMESTAMPTZ NOT NULL,
  request_count INTEGER NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (scope, subject_hash),
  CONSTRAINT api_rate_limits_count_check CHECK (request_count > 0),
  CONSTRAINT api_rate_limits_subject_hash_check CHECK (subject_hash ~ '^[a-f0-9]{64}$')
);

CREATE INDEX api_rate_limits_expiry_idx ON api_rate_limits (expires_at);

CREATE INDEX processing_jobs_terminal_completed_idx
  ON processing_jobs (completed_at, kind, state)
  WHERE state IN ('succeeded', 'failed', 'cancelled');
