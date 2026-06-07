CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users
ADD COLUMN IF NOT EXISTS is_subscriber BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE users
ADD COLUMN IF NOT EXISTS subscription_expires_at TIMESTAMPTZ;

ALTER TABLE users
ADD COLUMN IF NOT EXISTS subscription_cancelled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE users
ADD COLUMN IF NOT EXISTS english_level TEXT NOT NULL DEFAULT 'b1';

CREATE TABLE IF NOT EXISTS user_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_sessions_user_id_idx ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS user_sessions_expires_at_idx ON user_sessions (expires_at);

CREATE TABLE IF NOT EXISTS user_interests (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  interest_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, interest_id)
);

CREATE INDEX IF NOT EXISTS user_interests_user_id_idx ON user_interests (user_id);

CREATE TABLE IF NOT EXISTS recordings (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  topic TEXT NOT NULL,
  duration INTEGER NOT NULL,
  timestamp TIMESTAMPTZ NOT NULL,
  transcript TEXT NOT NULL,
  suggestions JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS practice_type TEXT NOT NULL DEFAULT 'topic';

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS audio_data_url TEXT;

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'ready';

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS processing_error TEXT;

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS photo_data_url TEXT;

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS photo_object TEXT;

CREATE INDEX IF NOT EXISTS recordings_user_id_timestamp_idx ON recordings (user_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS recording_upload_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  topic TEXT NOT NULL,
  duration INTEGER NOT NULL,
  timestamp TIMESTAMPTZ NOT NULL,
  practice_type TEXT NOT NULL DEFAULT 'topic',
  photo_data_url TEXT,
  photo_object TEXT,
  audio_extension TEXT,
  chunk_count INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'open',
  recording_id TEXT REFERENCES recordings(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS recording_upload_sessions_user_id_idx
  ON recording_upload_sessions (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feed_posts (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_recording_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  duration INTEGER NOT NULL,
  practice_type TEXT NOT NULL DEFAULT 'topic',
  audio_data_url TEXT,
  photo_data_url TEXT,
  photo_object TEXT,
  transcript TEXT NOT NULL,
  source_timestamp TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS feed_posts_user_source_recording_uidx
  ON feed_posts (user_id, source_recording_id);
CREATE INDEX IF NOT EXISTS feed_posts_created_at_idx
  ON feed_posts (created_at DESC);

CREATE TABLE IF NOT EXISTS feed_replies (
  id TEXT PRIMARY KEY,
  post_id TEXT NOT NULL REFERENCES feed_posts(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  duration INTEGER NOT NULL,
  audio_data_url TEXT,
  timestamp TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS feed_replies_post_created_idx
  ON feed_replies (post_id, created_at ASC);

CREATE TABLE IF NOT EXISTS feed_post_reactions (
  post_id TEXT NOT NULL REFERENCES feed_posts(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reaction TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (post_id, user_id)
);

CREATE INDEX IF NOT EXISTS feed_post_reactions_post_reaction_idx
  ON feed_post_reactions (post_id, reaction);

CREATE TABLE IF NOT EXISTS feed_reply_reactions (
  reply_id TEXT NOT NULL REFERENCES feed_replies(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reaction TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (reply_id, user_id)
);

CREATE INDEX IF NOT EXISTS feed_reply_reactions_reply_reaction_idx
  ON feed_reply_reactions (reply_id, reaction);
