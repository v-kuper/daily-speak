import { Pool, type QueryResult, type QueryResultRow } from "pg";

declare global {
  // eslint-disable-next-line no-var
  var __dailySpeakingPool: Pool | undefined;
  // eslint-disable-next-line no-var
  var __dailySpeakingSchemaPromise: Promise<void> | undefined;
}

const SCHEMA_SQL = `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_sessions_user_id_idx ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS user_sessions_expires_at_idx ON user_sessions (expires_at);
`;

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
  if (!globalThis.__dailySpeakingSchemaPromise) {
    globalThis.__dailySpeakingSchemaPromise = (async () => {
      const pool = getPool();
      try {
        await pool.query(SCHEMA_SQL);
      } catch (error) {
        // Allow a clean retry on the next request if initialization failed.
        globalThis.__dailySpeakingSchemaPromise = undefined;
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
