ALTER TABLE media_assets DROP CONSTRAINT media_assets_purpose_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_purpose_check CHECK (
  purpose IN ('recording_audio', 'recording_photo', 'shadowing_audio',
              'guest_preview_audio', 'interview_turn_audio')
);

ALTER TABLE processing_jobs DROP CONSTRAINT processing_jobs_kind_check;
ALTER TABLE processing_jobs ADD CONSTRAINT processing_jobs_kind_check CHECK (
  kind IN ('recording.process', 'shadowing.synthesize', 'media.delete',
           'guest.preview', 'interview.process')
);

CREATE TABLE interview_sessions (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  create_key TEXT NOT NULL,
  request_digest TEXT NOT NULL,
  user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
  topic TEXT NOT NULL,
  opening_question TEXT NOT NULL,
  english_level TEXT NOT NULL DEFAULT 'b1',
  interests JSONB NOT NULL DEFAULT '[]'::jsonb,
  useful_words JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL DEFAULT 'preparing',
  max_duration_seconds INTEGER NOT NULL,
  started_at TIMESTAMPTZ,
  ended_at_ms INTEGER,
  finalize_key TEXT,
  recording_id TEXT UNIQUE REFERENCES recordings(id) ON DELETE SET NULL,
  guest_preview_id TEXT UNIQUE REFERENCES guest_previews(id) ON DELETE SET NULL,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours'),
  CONSTRAINT interview_sessions_status_check CHECK
    (status IN ('preparing', 'ready', 'recording', 'finalizing', 'finalized', 'failed', 'cancelled')),
  CONSTRAINT interview_sessions_duration_check CHECK
    (max_duration_seconds BETWEEN 1 AND 600)
);

CREATE INDEX interview_sessions_owner_idx
  ON interview_sessions(owner_principal_id, created_at DESC);
CREATE UNIQUE INDEX interview_sessions_create_key_idx
  ON interview_sessions(owner_principal_id, create_key);
CREATE UNIQUE INDEX interview_sessions_one_active_idx
  ON interview_sessions(owner_principal_id)
  WHERE status IN ('preparing', 'ready', 'recording');
CREATE INDEX interview_sessions_expiry_idx
  ON interview_sessions(expires_at)
  WHERE status <> 'finalized';

ALTER TABLE media_uploads ADD COLUMN interview_session_id TEXT
  REFERENCES interview_sessions(id) ON DELETE SET NULL;
CREATE INDEX media_uploads_interview_session_idx
  ON media_uploads(interview_session_id)
  WHERE interview_session_id IS NOT NULL;

CREATE TABLE interview_candidates (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
  question TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('prepared', 'adaptive')),
  source_turn_seq INTEGER,
  consumed_by_turn_seq INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX interview_candidates_ready_idx
  ON interview_candidates(session_id, consumed_by_turn_seq, created_at);

CREATE TABLE interview_turns (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL CHECK (seq > 0),
  question TEXT NOT NULL,
  asked_at_ms INTEGER NOT NULL CHECK (asked_at_ms >= 0),
  ended_at_ms INTEGER CHECK (ended_at_ms >= asked_at_ms),
  audio_asset_id TEXT UNIQUE REFERENCES media_assets(id) ON DELETE SET NULL,
  advance_key TEXT,
  source_candidate_id TEXT REFERENCES interview_candidates(id) ON DELETE SET NULL,
  audio_idempotency_key TEXT,
  provisional_transcript TEXT,
  final_transcript TEXT,
  transcript_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (transcript_status IN ('pending', 'queued', 'ready', 'failed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(session_id, seq)
);
CREATE INDEX interview_turns_session_idx ON interview_turns(session_id, seq);
CREATE UNIQUE INDEX interview_turns_advance_key_idx
  ON interview_turns(session_id, advance_key) WHERE advance_key IS NOT NULL;
