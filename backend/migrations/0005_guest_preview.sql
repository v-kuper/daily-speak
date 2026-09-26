ALTER TABLE media_assets
DROP CONSTRAINT media_assets_purpose_check;

ALTER TABLE media_assets
ADD CONSTRAINT media_assets_purpose_check CHECK (
  purpose IN (
    'recording_audio',
    'recording_photo',
    'shadowing_audio',
    'feed_reply_audio',
    'guest_preview_audio'
  )
);

-- A guest may reserve only one live audio object. The index is the database
-- admission-control boundary for concurrent upload-intent requests; registered
-- users continue to have no per-owner asset uniqueness restriction.
CREATE UNIQUE INDEX media_assets_guest_preview_owner_uidx
  ON media_assets (owner_principal_id)
  WHERE purpose = 'guest_preview_audio' AND deleted_at IS NULL;

ALTER TABLE processing_jobs
DROP CONSTRAINT processing_jobs_kind_check;

ALTER TABLE processing_jobs
ADD CONSTRAINT processing_jobs_kind_check CHECK (
  kind IN ('recording.process', 'shadowing.synthesize', 'media.delete', 'guest.preview')
);

CREATE TABLE guest_previews (
  id TEXT PRIMARY KEY,
  guest_principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id) ON DELETE CASCADE,
  audio_asset_id TEXT NOT NULL UNIQUE REFERENCES media_assets(id) ON DELETE RESTRICT,
  topic TEXT NOT NULL,
  duration INTEGER NOT NULL,
  recording_timestamp TIMESTAMPTZ NOT NULL,
  practice_type TEXT NOT NULL DEFAULT 'free_talk',
  state TEXT NOT NULL DEFAULT 'queued',
  transcript TEXT NOT NULL DEFAULT '',
  preview_corrections JSONB NOT NULL DEFAULT '[]'::jsonb,
  processing_error TEXT,
  preview_job_id TEXT NOT NULL UNIQUE,
  idempotency_key TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  promoted_recording_id TEXT UNIQUE REFERENCES recordings(id) ON DELETE CASCADE,
  promoted_job_id TEXT UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  promoted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guest_previews_topic_check CHECK (length(btrim(topic)) > 0),
  CONSTRAINT guest_previews_duration_check CHECK (duration > 0),
  CONSTRAINT guest_previews_practice_type_check CHECK (
    practice_type IN ('free_talk', 'topic', 'photo_description')
  ),
  CONSTRAINT guest_previews_state_check CHECK (
    state IN ('queued', 'processing', 'ready', 'failed', 'promoted')
  ),
  CONSTRAINT guest_previews_corrections_check CHECK (
    jsonb_typeof(preview_corrections) = 'array'
    AND jsonb_array_length(preview_corrections) <= 2
  ),
  CONSTRAINT guest_previews_idempotency_key_check CHECK (
    length(btrim(idempotency_key)) > 0
  ),
  CONSTRAINT guest_previews_request_digest_check CHECK (
    request_digest ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT guest_previews_expiry_check CHECK (expires_at > created_at),
  CONSTRAINT guest_previews_promotion_shape_check CHECK (
    (
      state = 'promoted'
      AND promoted_recording_id IS NOT NULL
      AND promoted_job_id IS NOT NULL
      AND promoted_at IS NOT NULL
    )
    OR
    (
      state <> 'promoted'
      AND promoted_recording_id IS NULL
      AND promoted_job_id IS NULL
      AND promoted_at IS NULL
    )
  )
);

-- Consuming the guest-to-account hook is an account-level entitlement, not a
-- property of a device or anonymous principal. Keeping this small ledger
-- separate from recordings means deleting the promoted recording cannot make
-- the expensive onboarding pipeline available again.
CREATE TABLE guest_preview_entitlements (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  preview_id TEXT NOT NULL UNIQUE,
  consumed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX guest_previews_principal_idempotency_uidx
  ON guest_previews (guest_principal_id, idempotency_key);

CREATE INDEX guest_previews_state_expiry_idx
  ON guest_previews (state, expires_at)
  WHERE state <> 'promoted';
