import { randomUUID } from "node:crypto";
import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type PracticeType = "free_talk" | "topic" | "photo_description";

type CreateFeedPostPayload = {
  recordingId?: unknown;
};

type RecordingRow = {
  id: string;
  topic: string;
  duration: number;
  transcript: string;
  practice_type: PracticeType | string;
  audio_data_url: string | null;
  photo_data_url: string | null;
  photo_object: string | null;
  timestamp: string;
};

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

type IdRow = {
  id: string;
};

const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);
const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
const AUDIO_FILE_URL_PATTERN = /^\/uploads\/[a-z0-9/_-]+\.[a-z0-9]{2,10}$/i;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;
const MAX_AUDIO_UPLOAD_BYTES = 80 * 1024 * 1024;

const parseRecordingId = (payload: CreateFeedPostPayload | null): string => {
  if (typeof payload?.recordingId !== "string") {
    return "";
  }

  return payload.recordingId.trim();
};

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

const getFeedPostById = async (postId: string): Promise<FeedPostRow | null> => {
  const result = await query<FeedPostRow>(
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
    [postId]
  );

  return result.rows[0] ?? null;
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.feed.posts.get", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const result = await query<FeedPostRow>(
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
       GROUP BY p.id, u.email
       ORDER BY p.created_at DESC
       LIMIT 120`
    );

    const posts = result.rows.map((row) => toFeedPost(row));

    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      postsCount: posts.length
    });

    return NextResponse.json({ posts }, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load feed posts." }, { status: 500 });
  }
}

export async function POST(request: NextRequest) {
  const logger = createRouteLogger("api.feed.posts.post", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as CreateFeedPostPayload | null;
    const recordingId = parseRecordingId(payload);
    if (!recordingId) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_recording_id" });
      return NextResponse.json({ error: "Recording ID is required." }, { status: 400 });
    }

    const recordingResult = await query<RecordingRow>(
      `SELECT
         id,
         topic,
         duration,
         transcript,
         practice_type,
         audio_data_url,
         photo_data_url,
         photo_object,
         timestamp
       FROM recordings
       WHERE id = $1
         AND user_id = $2
       LIMIT 1`,
      [recordingId, user.id]
    );
    const recording = recordingResult.rows[0];
    if (!recording) {
      logger.warn("request.rejected", { status: 404, durationMs: elapsedMs(startedAt), reason: "recording_not_found" });
      return NextResponse.json({ error: "Recording not found." }, { status: 404 });
    }

    const insertResult = await query<IdRow>(
      `INSERT INTO feed_posts
         (id, user_id, source_recording_id, topic, duration, practice_type, audio_data_url, photo_data_url, photo_object, transcript, source_timestamp)
       VALUES
         ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
       ON CONFLICT (user_id, source_recording_id) DO NOTHING
       RETURNING id`,
      [
        randomUUID(),
        user.id,
        recording.id,
        recording.topic.slice(0, 300),
        Math.max(0, Number(recording.duration) || 0),
        normalizePracticeType(recording.practice_type),
        normalizeAudioDataUrl(recording.audio_data_url),
        normalizePhotoDataUrl(recording.photo_data_url),
        normalizePhotoObject(recording.photo_object),
        recording.transcript,
        new Date(recording.timestamp).toISOString()
      ]
    );

    const created = insertResult.rows.length > 0;
    const postId =
      insertResult.rows[0]?.id ??
      (
        await query<IdRow>(
          `SELECT id
           FROM feed_posts
           WHERE user_id = $1
             AND source_recording_id = $2
           LIMIT 1`,
          [user.id, recording.id]
        )
      ).rows[0]?.id;

    if (!postId) {
      logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), reason: "post_not_resolved" });
      return NextResponse.json({ error: "Failed to publish recording." }, { status: 500 });
    }

    const post = await getFeedPostById(postId);
    if (!post) {
      logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), reason: "post_not_found_after_save" });
      return NextResponse.json({ error: "Failed to publish recording." }, { status: 500 });
    }

    logger.info("request.success", {
      status: created ? 201 : 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      postId,
      recordingId: recording.id,
      created
    });

    return NextResponse.json({ post: toFeedPost(post) }, { status: created ? 201 : 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to publish recording." }, { status: 500 });
  }
}
