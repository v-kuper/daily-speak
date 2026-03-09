import { query } from "./db";

export const FEED_REACTION_VALUES = ["like", "love", "fire", "laugh", "support"] as const;
export type FeedReaction = (typeof FEED_REACTION_VALUES)[number];

export type FeedReactionCounts = Record<FeedReaction, number>;

export type FeedReactionSummary = {
  counts: FeedReactionCounts;
  currentReaction: FeedReaction | null;
};

type ReactionCountRow = {
  target_id: string;
  reaction: string;
  reaction_count: number | string;
};

type UserReactionRow = {
  target_id: string;
  reaction: string;
};

const FEED_REACTION_SET = new Set<string>(FEED_REACTION_VALUES);

export const normalizeFeedReaction = (value: unknown): FeedReaction | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().toLowerCase();
  return FEED_REACTION_SET.has(normalized) ? (normalized as FeedReaction) : null;
};

export const buildEmptyFeedReactionCounts = (): FeedReactionCounts => {
  return {
    like: 0,
    love: 0,
    fire: 0,
    laugh: 0,
    support: 0
  };
};

export const buildEmptyFeedReactionSummary = (): FeedReactionSummary => {
  return {
    counts: buildEmptyFeedReactionCounts(),
    currentReaction: null
  };
};

const toNonNegativeInt = (value: number | string): number => {
  const parsed = Number.parseInt(String(value), 10);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return 0;
  }

  return parsed;
};

const createSummaryMap = (targetIds: string[]): Record<string, FeedReactionSummary> => {
  const map: Record<string, FeedReactionSummary> = {};
  targetIds.forEach((id) => {
    map[id] = buildEmptyFeedReactionSummary();
  });
  return map;
};

const fillReactionCounts = (
  summaryMap: Record<string, FeedReactionSummary>,
  rows: ReactionCountRow[]
): void => {
  for (const row of rows) {
    const summary = summaryMap[row.target_id];
    if (!summary) {
      continue;
    }

    const reaction = normalizeFeedReaction(row.reaction);
    if (!reaction) {
      continue;
    }

    summary.counts[reaction] = toNonNegativeInt(row.reaction_count);
  }
};

const fillCurrentUserReaction = (
  summaryMap: Record<string, FeedReactionSummary>,
  rows: UserReactionRow[]
): void => {
  for (const row of rows) {
    const summary = summaryMap[row.target_id];
    if (!summary) {
      continue;
    }

    summary.currentReaction = normalizeFeedReaction(row.reaction);
  }
};

export const getFeedPostReactionSummaries = async (
  postIds: string[],
  userId: string
): Promise<Record<string, FeedReactionSummary>> => {
  if (postIds.length === 0) {
    return {};
  }

  const summaryMap = createSummaryMap(postIds);
  const [countsResult, userResult] = await Promise.all([
    query<ReactionCountRow>(
      `SELECT
         post_id AS target_id,
         reaction,
         COUNT(*)::int AS reaction_count
       FROM feed_post_reactions
       WHERE post_id = ANY($1::text[])
       GROUP BY post_id, reaction`,
      [postIds]
    ),
    query<UserReactionRow>(
      `SELECT
         post_id AS target_id,
         reaction
       FROM feed_post_reactions
       WHERE post_id = ANY($1::text[])
         AND user_id = $2`,
      [postIds, userId]
    )
  ]);

  fillReactionCounts(summaryMap, countsResult.rows);
  fillCurrentUserReaction(summaryMap, userResult.rows);

  return summaryMap;
};

export const getFeedReplyReactionSummaries = async (
  replyIds: string[],
  userId: string
): Promise<Record<string, FeedReactionSummary>> => {
  if (replyIds.length === 0) {
    return {};
  }

  const summaryMap = createSummaryMap(replyIds);
  const [countsResult, userResult] = await Promise.all([
    query<ReactionCountRow>(
      `SELECT
         reply_id AS target_id,
         reaction,
         COUNT(*)::int AS reaction_count
       FROM feed_reply_reactions
       WHERE reply_id = ANY($1::text[])
       GROUP BY reply_id, reaction`,
      [replyIds]
    ),
    query<UserReactionRow>(
      `SELECT
         reply_id AS target_id,
         reaction
       FROM feed_reply_reactions
       WHERE reply_id = ANY($1::text[])
         AND user_id = $2`,
      [replyIds, userId]
    )
  ]);

  fillReactionCounts(summaryMap, countsResult.rows);
  fillCurrentUserReaction(summaryMap, userResult.rows);

  return summaryMap;
};
