import { query } from "./db";

export const FREE_WEEKLY_LIMIT_SECONDS = 10 * 60;
export const SUBSCRIBER_MAX_SESSION_SECONDS = 10 * 60;

export type RecordingQuota = {
  isSubscriber: boolean;
  weeklyLimitSeconds: number | null;
  weeklyUsedSeconds: number;
  weeklyRemainingSeconds: number | null;
  maxSessionSeconds: number;
};

type UserPlanRow = {
  is_subscriber: boolean;
};

type UsageRow = {
  used_seconds: number | string | null;
};

const toNonNegativeInt = (value: number | string | null | undefined): number => {
  const parsed = Number.parseInt(String(value ?? 0), 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return 0;
  }

  return parsed;
};

const buildQuota = (isSubscriber: boolean, weeklyUsedSeconds: number): RecordingQuota => {
  if (isSubscriber) {
    return {
      isSubscriber: true,
      weeklyLimitSeconds: null,
      weeklyUsedSeconds,
      weeklyRemainingSeconds: null,
      maxSessionSeconds: SUBSCRIBER_MAX_SESSION_SECONDS
    };
  }

  return {
    isSubscriber: false,
    weeklyLimitSeconds: FREE_WEEKLY_LIMIT_SECONDS,
    weeklyUsedSeconds,
    weeklyRemainingSeconds: Math.max(0, FREE_WEEKLY_LIMIT_SECONDS - weeklyUsedSeconds),
    maxSessionSeconds: SUBSCRIBER_MAX_SESSION_SECONDS
  };
};

const getIsSubscriber = async (userId: string): Promise<boolean> => {
  const userResult = await query<UserPlanRow>(
    `SELECT (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber
     FROM users
     WHERE id = $1
     LIMIT 1`,
    [userId]
  );

  const row = userResult.rows[0];
  return row ? Boolean(row.is_subscriber) : false;
};

const getWeeklyUsageSeconds = async (userId: string): Promise<number> => {
  const usageResult = await query<UsageRow>(
    `SELECT COALESCE(SUM(duration), 0) AS used_seconds
     FROM recordings
     WHERE user_id = $1
       AND created_at >= date_trunc('week', NOW())
       AND created_at < date_trunc('week', NOW()) + INTERVAL '1 week'`,
    [userId]
  );

  return toNonNegativeInt(usageResult.rows[0]?.used_seconds);
};

export const getRecordingQuota = async (
  userId: string,
  options?: { isSubscriber?: boolean }
): Promise<RecordingQuota> => {
  const isSubscriber = typeof options?.isSubscriber === "boolean" ? options.isSubscriber : await getIsSubscriber(userId);
  const weeklyUsedSeconds = await getWeeklyUsageSeconds(userId);

  return buildQuota(isSubscriber, weeklyUsedSeconds);
};
