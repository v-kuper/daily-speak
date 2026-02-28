import { createHash } from "node:crypto";
import { Pool, type QueryResult, type QueryResultRow } from "pg";

declare global {
  // eslint-disable-next-line no-var
  var __dailySpeakingPool: Pool | undefined;
  // eslint-disable-next-line no-var
  var __dailySpeakingSchemaPromise: Promise<void> | undefined;
  // eslint-disable-next-line no-var
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

CREATE INDEX IF NOT EXISTS recordings_user_id_timestamp_idx ON recordings (user_id, timestamp DESC);
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
