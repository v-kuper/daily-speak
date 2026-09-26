CREATE TABLE processing_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  state TEXT NOT NULL DEFAULT 'queued',
  priority INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_token TEXT,
  lease_owner TEXT,
  lease_expires_at TIMESTAMPTZ,
  heartbeat_at TIMESTAMPTZ,
  last_error TEXT,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT processing_jobs_kind_check CHECK (
    kind IN ('recording.process', 'shadowing.synthesize', 'media.delete')
  ),
  CONSTRAINT processing_jobs_state_check CHECK (
    state IN ('queued', 'running', 'retry_wait', 'succeeded', 'failed', 'cancelled')
  ),
  CONSTRAINT processing_jobs_attempts_check CHECK (
    attempts >= 0 AND max_attempts > 0 AND attempts <= max_attempts
  )
);

CREATE INDEX processing_jobs_claim_idx
  ON processing_jobs (kind, state, priority DESC, available_at ASC, created_at ASC)
  WHERE state IN ('queued', 'retry_wait', 'running');

CREATE INDEX processing_jobs_resource_idx
  ON processing_jobs (resource_id, created_at DESC);

ALTER TABLE recordings
ADD COLUMN processing_job_id TEXT;

CREATE INDEX recordings_processing_job_id_idx
  ON recordings (processing_job_id)
  WHERE processing_job_id IS NOT NULL;

-- Preserve cleanup work queued by older releases. md5 is used only to create a
-- stable identifier; it is not used for authentication or integrity.
INSERT INTO processing_jobs
  (id, kind, resource_id, idempotency_key, payload, max_attempts, created_at, updated_at)
SELECT
  'legacy-media-delete-' || md5(public_url),
  'media.delete',
  public_url,
  'media.delete:' || public_url,
  jsonb_build_object('publicUrl', public_url),
  20,
  created_at,
  updated_at
FROM pending_file_deletions
ON CONFLICT (idempotency_key) DO NOTHING;
