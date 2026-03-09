import { randomUUID } from "node:crypto";
import { mkdir, unlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../../../src/server/auth";
import { buildEmptyFeedReactionSummary } from "../../../../../../src/server/feedReactions";
import {
  FREE_WEEKLY_LIMIT_SECONDS,
  SUBSCRIBER_MAX_SESSION_SECONDS,
  getRecordingQuota
} from "../../../../../../src/server/recordingQuota";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type CreateReplyPayload = {
  duration?: unknown;
  audioDataUrl?: unknown;
  timestamp?: unknown;
};

type FeedReplyRow = {
  id: string;
  post_id: string;
  duration: number;
  audio_data_url: string | null;
  timestamp: string;
  created_at: string;
};

type IdRow = {
  id: string;
};

type ParsedIncomingAudioDataUrl = {
  base64: string;
  extension: string;
  normalizedDataUrl: string;
};

type SavedAudioFile = {
  publicUrl: string;
  absolutePath: string;
};

const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
const MAX_AUDIO_UPLOAD_BYTES = 80 * 1024 * 1024;
const AUDIO_PUBLIC_BASE_URL = "/uploads/feed-replies";
const AUDIO_STORAGE_ROOT_DIR = path.join(process.cwd(), "public", "uploads", "feed-replies");
const AUDIO_EXTENSION_BY_MIME = new Map<string, string>([
  ["audio/webm", "webm"],
  ["video/webm", "webm"],
  ["audio/mp4", "m4a"],
  ["audio/x-m4a", "m4a"],
  ["video/mp4", "m4a"],
  ["audio/ogg", "ogg"],
  ["video/ogg", "ogg"],
  ["audio/wav", "wav"],
  ["audio/x-wav", "wav"],
  ["audio/vnd.wave", "wav"],
  ["audio/mpeg", "mp3"]
]);

const formatSeconds = (seconds: number): string => {
  const normalized = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(normalized / 60);
  const restSeconds = normalized % 60;
  return `${minutes}:${restSeconds.toString().padStart(2, "0")}`;
};

const sanitizePathSegment = (value: string): string => {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, "_").slice(0, 80);
  return normalized || "user";
};

const parseDuration = (value: unknown): number => {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }

  return Math.max(0, parsed);
};

const parseTimestamp = (value: unknown): Date => {
  if (typeof value !== "string") {
    return new Date();
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return new Date();
  }

  return parsed;
};

const resolveAudioExtension = (baseMimeType: string): string | null => {
  const mapped = AUDIO_EXTENSION_BY_MIME.get(baseMimeType);
  if (mapped) {
    return mapped;
  }

  if (!baseMimeType.startsWith("audio/") && !baseMimeType.startsWith("video/")) {
    return null;
  }

  const subtypeRaw = baseMimeType.slice("audio/".length).toLowerCase();
  if (!subtypeRaw) {
    return null;
  }

  const normalizedSubtype = subtypeRaw.replace(/^x-/, "");
  if (normalizedSubtype === "mpeg") {
    return "mp3";
  }
  if (normalizedSubtype === "mp4") {
    return "m4a";
  }
  if (normalizedSubtype === "wave") {
    return "wav";
  }

  const cleaned = normalizedSubtype.replace(/[^a-z0-9]+/g, "");
  if (!cleaned || cleaned.length > 10) {
    return null;
  }

  return cleaned;
};

const parseIncomingAudioDataUrl = (value: unknown): ParsedIncomingAudioDataUrl | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  const match = normalized.match(AUDIO_DATA_URL_PATTERN);
  if (!match) {
    return null;
  }

  const mediaType = match[1].toLowerCase().replace(/\s+/g, "");
  const baseMimeType = mediaType.split(";", 1)[0];
  const extension = resolveAudioExtension(baseMimeType);
  if (!extension) {
    return null;
  }
  const base64 = match[2].trim().replace(/-/g, "+").replace(/_/g, "/");
  if (!/^[A-Za-z0-9+/=]+$/.test(base64)) {
    return null;
  }
  const padding = base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((base64.length * 3) / 4) - padding;
  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > MAX_AUDIO_UPLOAD_BYTES) {
    return null;
  }

  return {
    normalizedDataUrl: `data:${mediaType};base64,${base64}`,
    base64,
    extension
  };
};

const saveAudioFile = async (userId: string, replyId: string, audio: ParsedIncomingAudioDataUrl): Promise<SavedAudioFile> => {
  const userDir = sanitizePathSegment(userId);
  const fileName = `${replyId}.${audio.extension}`;
  const directory = path.join(AUDIO_STORAGE_ROOT_DIR, userDir);
  const absolutePath = path.join(directory, fileName);
  const publicUrl = `${AUDIO_PUBLIC_BASE_URL}/${userDir}/${fileName}`;
  const buffer = Buffer.from(audio.base64, "base64");

  if (buffer.length <= 0 || buffer.length > MAX_AUDIO_UPLOAD_BYTES) {
    throw new Error("Audio payload is invalid.");
  }

  await mkdir(directory, { recursive: true });
  await writeFile(absolutePath, buffer);

  return { publicUrl, absolutePath };
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

const toFeedReply = (row: FeedReplyRow, authorEmail: string) => {
  return {
    id: row.id,
    postId: row.post_id,
    duration: Math.max(0, Number(row.duration) || 0),
    audioDataUrl: row.audio_data_url,
    timestamp: new Date(row.timestamp).toISOString(),
    createdAt: new Date(row.created_at).toISOString(),
    authorMaskedEmail: maskEmail(authorEmail),
    reactions: buildEmptyFeedReactionSummary()
  };
};

export async function POST(
  request: NextRequest,
  context: { params: Promise<{ postId: string }> }
) {
  const logger = createRouteLogger("api.feed.posts.replies.post", request);
  const startedAt = Date.now();
  let savedAudioAbsolutePath: string | null = null;

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

    const postResult = await query<IdRow>(
      `SELECT id
       FROM feed_posts
       WHERE id = $1
       LIMIT 1`,
      [normalizedPostId]
    );
    if (!postResult.rows[0]) {
      logger.warn("request.rejected", { status: 404, durationMs: elapsedMs(startedAt), reason: "post_not_found", postId: normalizedPostId });
      return NextResponse.json({ error: "Feed post not found." }, { status: 404 });
    }

    const payload = (await request.json().catch(() => null)) as CreateReplyPayload | null;
    const duration = parseDuration(payload?.duration);
    if (!duration) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_duration" });
      return NextResponse.json({ error: "Reply duration is invalid." }, { status: 400 });
    }

    const parsedAudioDataUrl = parseIncomingAudioDataUrl(payload?.audioDataUrl);
    if (!parsedAudioDataUrl) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_audio" });
      return NextResponse.json({ error: "Voice reply is required." }, { status: 400 });
    }

    const quotaBefore = await getRecordingQuota(user.id, { isSubscriber: user.isSubscriber });
    if (quotaBefore.isSubscriber) {
      if (duration > SUBSCRIBER_MAX_SESSION_SECONDS) {
        logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "subscriber_session_limit" });
        return NextResponse.json(
          { error: "Subscribers can save recordings up to 10:00 per session." },
          { status: 400 }
        );
      }
    } else {
      const remaining = quotaBefore.weeklyRemainingSeconds ?? 0;
      if (duration > remaining) {
        logger.warn("request.rejected", { status: 403, durationMs: elapsedMs(startedAt), reason: "weekly_quota_exceeded" });
        return NextResponse.json(
          {
            error: `Weekly free limit exceeded. You have ${formatSeconds(remaining)} left out of ${formatSeconds(
              FREE_WEEKLY_LIMIT_SECONDS
            )} this week.`
          },
          { status: 403 }
        );
      }
    }

    const timestamp = parseTimestamp(payload?.timestamp);
    const replyId = randomUUID();
    const savedAudio = await saveAudioFile(user.id, replyId, parsedAudioDataUrl);
    savedAudioAbsolutePath = savedAudio.absolutePath;
    logger.info("audio.saved", { userId: user.id, postId: normalizedPostId, replyId, audioUrl: savedAudio.publicUrl });

    const insertResult = await query<FeedReplyRow>(
      `INSERT INTO feed_replies
         (id, post_id, user_id, duration, audio_data_url, timestamp)
       VALUES
         ($1, $2, $3, $4, $5, $6)
       RETURNING id, post_id, duration, audio_data_url, timestamp, created_at`,
      [replyId, normalizedPostId, user.id, duration, savedAudio.publicUrl, timestamp.toISOString()]
    );
    savedAudioAbsolutePath = null;

    const quota = await getRecordingQuota(user.id, { isSubscriber: user.isSubscriber });
    const reply = toFeedReply(insertResult.rows[0], user.email);

    logger.info("request.success", {
      status: 201,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      postId: normalizedPostId,
      replyId
    });

    return NextResponse.json({ reply, quota }, { status: 201 });
  } catch (error) {
    if (savedAudioAbsolutePath) {
      await unlink(savedAudioAbsolutePath).catch(() => undefined);
      logger.warn("audio.cleanup", { durationMs: elapsedMs(startedAt), removedPath: savedAudioAbsolutePath });
    }

    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to save voice reply." }, { status: 500 });
  }
}
