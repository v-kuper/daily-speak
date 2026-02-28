import { query } from "./db";

export type SubscriptionState = {
  isSubscriber: boolean;
  subscriptionExpiresAt: string | null;
  subscriptionCancelled: boolean;
};

type SubscriptionRow = {
  is_subscriber_active: boolean;
  subscription_expires_at: string | Date | null;
  subscription_cancelled: boolean;
};

const toIsoOrNull = (value: string | Date | null): string | null => {
  if (!value) {
    return null;
  }

  const parsed = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return null;
  }

  return parsed.toISOString();
};

const mapSubscriptionRow = (row: SubscriptionRow | undefined): SubscriptionState => {
  if (!row) {
    return {
      isSubscriber: false,
      subscriptionExpiresAt: null,
      subscriptionCancelled: false
    };
  }

  const isSubscriber = Boolean(row.is_subscriber_active);
  const subscriptionExpiresAt = toIsoOrNull(row.subscription_expires_at);

  return {
    isSubscriber,
    subscriptionExpiresAt,
    subscriptionCancelled: isSubscriber ? Boolean(row.subscription_cancelled) : false
  };
};

export const getSubscriptionState = async (userId: string): Promise<SubscriptionState> => {
  const result = await query<SubscriptionRow>(
    `SELECT
       (is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())) AS is_subscriber_active,
       subscription_expires_at,
       subscription_cancelled
     FROM users
     WHERE id = $1
     LIMIT 1`,
    [userId]
  );

  return mapSubscriptionRow(result.rows[0]);
};
