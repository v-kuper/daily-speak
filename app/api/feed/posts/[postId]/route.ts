import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type PracticeType = "free_talk" | "topic" | "photo_description";

type FeedPostRow = {
  id: string;
  source_recording_id: string;
  topic: string;
  duration: number;
  practice_type: PracticeType | string;
  audio_data_url: string | null;
  photo_data_url: string | null;
  photo_object: string | null;
  transcript: string;
  source_timestamp: string;
  created_at: string;
  author_email: string;
  reply_count: number | string;
};

type FeedReplyRow = {
  id: string;
  post_id: string;
  duration: number;
  audio_data_url: string | null;
  timestamp: string;
  created_at: string;
  author_email: string;
};

const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);
const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
const AUDIO_FILE_URL_PATTERN = /^\/uploads\/[a-z0-9/_-]+\.[a-z0-9]{2,10}$/i;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;
const MAX_AUDIO_UPLOAD_BYTES = 80 * 1024 * 1024;

const normalizePracticeType = (value: unknown): PracticeType => {
  if (typeof value !== "string") {
    return "topic";
  }

  const normalized = value.trim().toLowerCase() as PracticeType;
  return PRACTICE_TYPE_SET.has(normalized) ? normalized : "topic";
};

const normalizeAudioDataUrl = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  if (AUDIO_FILE_URL_PATTERN.test(normalized)) {
    return normalized;
  }

  const match = normalized.match(AUDIO_DATA_URL_PATTERN);
  if (!match) {
    return null;
  }

  const mediaType = match[1].toLowerCase().replace(/\s+/g, "");
  const payload = match[2].trim().replace(/-/g, "+").replace(/_/g, "/");
  if (!/^[A-Za-z0-9+/=]+$/.test(payload)) {
    return null;
  }
  const padding = payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((payload.length * 3) / 4) - padding;
  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > MAX_AUDIO_UPLOAD_BYTES) {
    return null;
  }

  return `data:${mediaType};base64,${payload}`;
};

const normalizePhotoDataUrl = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  if (!PHOTO_DATA_URL_PATTERN.test(normalized)) {
    return null;
  }

  return normalized;
};

const normalizePhotoObject = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().replace(/\s+/g, " ").slice(0, 120);
  return normalized || null;
};

const toReplyCount = (value: number | string): number => {
  const parsed = Number.parseInt(String(value), 10);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
};

const maskEmail = (value: string): string => {
  const [localRaw, domainRaw] = value.toLowerCase().split("@");
  const local = (localRaw ?? "").trim();
  const domain = (domainRaw ?? "").trim();

  if (!local || !domain) {
    return "us***@hidden";
  }

  if (local.length <= 2) {
    return `${local[0] ?? "u"}***@${domain}`;
  }

  return `${local.slice(0, 2)}***@${domain}`;
};

const toFeedPost = (row: FeedPostRow) => {
  return {
    id: row.id,
    sourceRecordingId: row.source_recording_id,
    topic: row.topic,
    duration: Math.max(0, Number(row.duration) || 0),
    practiceType: normalizePracticeType(row.practice_type),
    audioDataUrl: normalizeAudioDataUrl(row.audio_data_url),
    photoDataUrl: normalizePhotoDataUrl(row.photo_data_url),
    photoObject: normalizePhotoObject(row.photo_object),
    transcript: row.transcript,
    sourceTimestamp: new Date(row.source_timestamp).toISOString(),
    createdAt: new Date(row.created_at).toISOString(),
    authorMaskedEmail: maskEmail(row.author_email),
    replyCount: toReplyCount(row.reply_count)
  };
};

const toFeedReply = (row: FeedReplyRow) => {
  return {
    id: row.id,
    postId: row.post_id,
    duration: Math.max(0, Number(row.duration) || 0),
    audioDataUrl: normalizeAudioDataUrl(row.audio_data_url),
    timestamp: new Date(row.timestamp).toISOString(),
    createdAt: new Date(row.created_at).toISOString(),
    authorMaskedEmail: maskEmail(row.author_email)
  };
};

export async function GET(
  request: NextRequest,
  context: { params: Promise<{ postId: string }> }
) {
  const logger = createRouteLogger("api.feed.posts.by-id.get", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const { postId } = await context.params;
    const normalizedPostId = typeof postId === "string" ? postId.trim() : "";
    if (!normalizedPostId) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_post_id" });
      return NextResponse.json({ error: "Post ID is required." }, { status: 400 });
    }

    const postResult = await query<FeedPostRow>(
      `SELECT
         p.id,
         p.source_recording_id,
         p.topic,
         p.duration,
         p.practice_type,
         p.audio_data_url,
         p.photo_data_url,
         p.photo_object,
         p.transcript,
         p.source_timestamp,
         p.created_at,
         u.email AS author_email,
         COALESCE(COUNT(r.id), 0)::int AS reply_count
       FROM feed_posts p
       JOIN users u ON u.id = p.user_id
       LEFT JOIN feed_replies r ON r.post_id = p.id
       WHERE p.id = $1
       GROUP BY p.id, u.email
       LIMIT 1`,
      [normalizedPostId]
    );
    const post = postResult.rows[0];
    if (!post) {
      logger.warn("request.rejected", { status: 404, durationMs: elapsedMs(startedAt), reason: "post_not_found", postId: normalizedPostId });
      return NextResponse.json({ error: "Feed post not found." }, { status: 404 });
    }

    const repliesResult = await query<FeedReplyRow>(
      `SELECT
         r.id,
         r.post_id,
         r.duration,
         r.audio_data_url,
         r.timestamp,
         r.created_at,
         u.email AS author_email
       FROM feed_replies r
       JOIN users u ON u.id = r.user_id
       WHERE r.post_id = $1
       ORDER BY r.created_at ASC`,
      [normalizedPostId]
    );
    const replies = repliesResult.rows.map((row) => toFeedReply(row));

    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      postId: normalizedPostId,
      repliesCount: replies.length
    });

    return NextResponse.json({ post: toFeedPost(post), replies }, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load feed thread." }, { status: 500 });
  }
}
