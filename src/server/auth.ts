import { createHash, randomBytes, randomUUID, scryptSync, timingSafeEqual } from "node:crypto";
import { DEFAULT_ENGLISH_LEVEL, normalizeEnglishLevel, type EnglishLevel } from "../lib/englishLevel";
import { query } from "./db";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const PASSWORD_MIN_LENGTH = 8;
const SESSION_TTL_MS = 1000 * 60 * 60 * 24 * 30;

const SCRYPT_N = 16384;
const SCRYPT_R = 8;
const SCRYPT_P = 1;
const SCRYPT_KEY_LENGTH = 64;

type UserRow = {
  id: string;
  email: string;
  password_hash: string;
  is_subscriber: boolean;
  english_level: string | null;
};

type SessionUserRow = {
  user_id: string;
  email: string;
  is_subscriber: boolean;
  english_level: string | null;
};

type PgLikeError = {
  code?: string;
};

export type AuthUser = {
  id: string;
  email: string;
  isSubscriber: boolean;
  englishLevel: EnglishLevel;
};

export type SessionData = {
  token: string;
  expiresAt: Date;
};

export class AuthError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

const isPgError = (value: unknown): value is PgLikeError => {
  return typeof value === "object" && value !== null;
};

export const normalizeEmail = (rawEmail: string): string => {
  return rawEmail.trim().toLowerCase();
};

const hashSessionToken = (token: string): string => {
  return createHash("sha256").update(token).digest("hex");
};

const hashPassword = (password: string): string => {
  const salt = randomBytes(16).toString("hex");
  const hash = scryptSync(password, salt, SCRYPT_KEY_LENGTH, {
    N: SCRYPT_N,
    r: SCRYPT_R,
    p: SCRYPT_P,
    maxmem: 64 * 1024 * 1024
  }).toString("hex");

  return ["scrypt", String(SCRYPT_N), String(SCRYPT_R), String(SCRYPT_P), salt, hash].join("$");
};

const verifyPassword = (password: string, encodedHash: string): boolean => {
  const parts = encodedHash.split("$");
  if (parts.length !== 6 || parts[0] !== "scrypt") {
    return false;
  }

  const [, nRaw, rRaw, pRaw, salt, storedHash] = parts;
  const n = Number.parseInt(nRaw, 10);
  const r = Number.parseInt(rRaw, 10);
  const p = Number.parseInt(pRaw, 10);
  if (!Number.isFinite(n) || !Number.isFinite(r) || !Number.isFinite(p) || !salt || !storedHash) {
    return false;
  }

  const computed = scryptSync(password, salt, storedHash.length / 2, {
    N: n,
    r,
    p,
    maxmem: 64 * 1024 * 1024
  });
  const stored = Buffer.from(storedHash, "hex");

  if (stored.length !== computed.length) {
    return false;
  }

  return timingSafeEqual(computed, stored);
};

export const validateCredentials = (email: string, password: string): { email: string; password: string } => {
  const normalizedEmail = normalizeEmail(email);
  if (!EMAIL_PATTERN.test(normalizedEmail)) {
    throw new AuthError("Enter a valid email address.", 400);
  }

  const normalizedPassword = password.trim();
  if (normalizedPassword.length < PASSWORD_MIN_LENGTH) {
    throw new AuthError(`Password must be at least ${PASSWORD_MIN_LENGTH} characters.`, 400);
  }

  return {
    email: normalizedEmail,
    password: normalizedPassword
  };
};

export const registerUser = async (email: string, password: string): Promise<AuthUser> => {
  const id = randomUUID();
  const passwordHash = hashPassword(password);

  try {
    await query(
      `INSERT INTO users (id, email, password_hash)
       VALUES ($1, $2, $3)`,
      [id, email, passwordHash]
    );
  } catch (error) {
    if (isPgError(error) && error.code === "23505") {
      throw new AuthError("User with this email already exists.", 409);
    }

    throw error;
  }

  return {
    id,
    email,
    isSubscriber: false,
    englishLevel: DEFAULT_ENGLISH_LEVEL
  };
};

export const loginUser = async (email: string, password: string): Promise<AuthUser> => {
  const result = await query<UserRow>(
    `SELECT
       id,
       email,
       password_hash,
       (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber,
       english_level
     FROM users
     WHERE email = $1
     LIMIT 1`,
    [email]
  );

  const user = result.rows[0];
  if (!user || !verifyPassword(password, user.password_hash)) {
    throw new AuthError("Invalid email or password.", 401);
  }

  return {
    id: user.id,
    email: user.email,
    isSubscriber: Boolean(user.is_subscriber),
    englishLevel: normalizeEnglishLevel(user.english_level)
  };
};

export const createSession = async (userId: string): Promise<SessionData> => {
  const sessionId = randomUUID();
  const token = randomBytes(32).toString("hex");
  const tokenHash = hashSessionToken(token);
  const expiresAt = new Date(Date.now() + SESSION_TTL_MS);

  await query(
    `INSERT INTO user_sessions (id, user_id, token_hash, expires_at)
     VALUES ($1, $2, $3, $4)`,
    [sessionId, userId, tokenHash, expiresAt.toISOString()]
  );

  await query("DELETE FROM user_sessions WHERE expires_at <= NOW()", []);

  return {
    token,
    expiresAt
  };
};

export const getUserBySessionToken = async (token: string | null | undefined): Promise<AuthUser | null> => {
  if (!token) {
    return null;
  }

  const tokenHash = hashSessionToken(token);
  const result = await query<SessionUserRow>(
    `SELECT
       s.user_id,
       u.email,
       (u.is_subscriber AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > NOW())) AS is_subscriber,
       u.english_level
     FROM user_sessions s
     JOIN users u ON u.id = s.user_id
     WHERE s.token_hash = $1
       AND s.expires_at > NOW()
     LIMIT 1`,
    [tokenHash]
  );

  const row = result.rows[0];
  if (!row) {
    return null;
  }

  return {
    id: row.user_id,
    email: row.email,
    isSubscriber: Boolean(row.is_subscriber),
    englishLevel: normalizeEnglishLevel(row.english_level)
  };
};

export const deleteSessionByToken = async (token: string | null | undefined): Promise<void> => {
  if (!token) {
    return;
  }

  const tokenHash = hashSessionToken(token);
  await query("DELETE FROM user_sessions WHERE token_hash = $1", [tokenHash]);
};

export const SESSION_COOKIE_NAME = "daily_speaking_session";

export const getSessionCookieOptions = (expiresAt?: Date) => {
  return {
    httpOnly: true,
    sameSite: "lax" as const,
    secure: process.env.NODE_ENV === "production",
    path: "/",
    ...(expiresAt ? { expires: expiresAt } : {})
  };
};
