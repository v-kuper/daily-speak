import { createHash } from "node:crypto";
import { Pool, type QueryResult, type QueryResultRow } from "pg";

declare global {
  var __dailySpeakingPool: Pool | undefined;
  var __dailySpeakingSchemaPromise: Promise<void> | undefined;
  var __dailySpeakingSchemaHash: string | undefined;
}

const SCHEMA_SQL = `
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
ADD COLUMN IF NOT EXISTS preferred_ollama_model TEXT;

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
ADD COLUMN IF NOT EXISTS photo_data_url TEXT;

ALTER TABLE recordings
ADD COLUMN IF NOT EXISTS photo_object TEXT;

CREATE INDEX IF NOT EXISTS recordings_user_id_timestamp_idx ON recordings (user_id, timestamp DESC);

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
`;

const SCHEMA_HASH = createHash("sha256").update(SCHEMA_SQL).digest("hex");

const getPool = (): Pool => {
  if (globalThis.__dailySpeakingPool) {
    return globalThis.__dailySpeakingPool;
  }

  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error("DATABASE_URL is required to use PostgreSQL.");
  }

  const sslMode = (process.env.DATABASE_SSL ?? "").trim().toLowerCase();
  const useSsl = sslMode === "require" || sslMode === "true";

  const pool = new Pool({
    connectionString,
    ssl: useSsl
      ? {
          rejectUnauthorized: false
        }
      : undefined
  });

  globalThis.__dailySpeakingPool = pool;
  return pool;
};

export const ensureSchema = async (): Promise<void> => {
  const schemaChanged = globalThis.__dailySpeakingSchemaHash !== SCHEMA_HASH;

  if (schemaChanged || !globalThis.__dailySpeakingSchemaPromise) {
    globalThis.__dailySpeakingSchemaHash = SCHEMA_HASH;
    globalThis.__dailySpeakingSchemaPromise = (async () => {
      const pool = getPool();
      try {
        await pool.query(SCHEMA_SQL);
      } catch (error) {
        // Allow a clean retry on the next request if initialization failed.
        globalThis.__dailySpeakingSchemaPromise = undefined;
        globalThis.__dailySpeakingSchemaHash = undefined;
        throw error;
      }
    })();
  }

  await globalThis.__dailySpeakingSchemaPromise;
};

export const query = async <T extends QueryResultRow = QueryResultRow>(
  sql: string,
  params: unknown[] = []
): Promise<QueryResult<T>> => {
  await ensureSchema();
  return getPool().query<T>(sql, params);
};
