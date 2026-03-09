import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../../../src/server/auth";
import {
  buildEmptyFeedReactionSummary,
  getFeedReplyReactionSummaries,
  normalizeFeedReaction
} from "../../../../../../src/server/feedReactions";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type ReactionPayload = {
  reaction?: unknown;
};

type IdRow = {
  id: string;
};

const hasReactionField = (payload: ReactionPayload | null): boolean => {
  if (!payload || typeof payload !== "object") {
    return false;
  }

  return Object.prototype.hasOwnProperty.call(payload, "reaction");
};

const shouldClearReaction = (value: unknown): boolean => {
  return value === null || (typeof value === "string" && value.trim().length === 0);
};

export async function POST(
  request: NextRequest,
  context: { params: Promise<{ replyId: string }> }
) {
  const logger = createRouteLogger("api.feed.replies.reactions.post", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const { replyId } = await context.params;
    const normalizedReplyId = typeof replyId === "string" ? replyId.trim() : "";
    if (!normalizedReplyId) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_reply_id" });
      return NextResponse.json({ error: "Reply ID is required." }, { status: 400 });
    }

    const replyResult = await query<IdRow>(
      `SELECT id
       FROM feed_replies
       WHERE id = $1
       LIMIT 1`,
      [normalizedReplyId]
    );
    if (!replyResult.rows[0]) {
      logger.warn("request.rejected", {
        status: 404,
        durationMs: elapsedMs(startedAt),
        reason: "reply_not_found",
        replyId: normalizedReplyId
      });
      return NextResponse.json({ error: "Feed reply not found." }, { status: 404 });
    }

    const payload = (await request.json().catch(() => null)) as ReactionPayload | null;
    if (!hasReactionField(payload)) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_reaction" });
      return NextResponse.json({ error: "Reaction is required." }, { status: 400 });
    }

    if (shouldClearReaction(payload?.reaction)) {
      await query(
        `DELETE FROM feed_reply_reactions
         WHERE reply_id = $1
           AND user_id = $2`,
        [normalizedReplyId, user.id]
      );
    } else {
      const reaction = normalizeFeedReaction(payload?.reaction);
      if (!reaction) {
        logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_reaction" });
        return NextResponse.json({ error: "Reaction is invalid." }, { status: 400 });
      }

      await query(
        `INSERT INTO feed_reply_reactions (reply_id, user_id, reaction)
         VALUES ($1, $2, $3)
         ON CONFLICT (reply_id, user_id) DO UPDATE
           SET reaction = EXCLUDED.reaction,
               created_at = NOW()`,
        [normalizedReplyId, user.id, reaction]
      );
    }

    const reactionMap = await getFeedReplyReactionSummaries([normalizedReplyId], user.id);
    const reactions = reactionMap[normalizedReplyId] ?? buildEmptyFeedReactionSummary();

    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      replyId: normalizedReplyId
    });

    return NextResponse.json({ reactions }, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to update reply reaction." }, { status: 500 });
  }
}
