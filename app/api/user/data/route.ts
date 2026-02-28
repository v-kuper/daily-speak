import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { getRecordingQuota } from "../../../../src/server/recordingQuota";
import { getSubscriptionState } from "../../../../src/server/subscription";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type InterestRow = {
  interest_id: string;
};

type PracticeType = "free_talk" | "topic" | "photo_description";

type RecordingRow = {
  id: string;
  topic: string;
  duration: number;
  timestamp: string;
  transcript: string;
  suggestions: unknown;
  practice_type: PracticeType | string;
  audio_data_url: string | null;
  photo_data_url: string | null;
  photo_object: string | null;
};

const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);
const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
const AUDIO_FILE_URL_PATTERN = /^\/uploads\/recordings\/[a-z0-9/_-]+\.[a-z0-9]{2,10}$/i;
const MAX_AUDIO_UPLOAD_BYTES = 80 * 1024 * 1024;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;

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
  const base64 = match[2].trim().replace(/-/g, "+").replace(/_/g, "/");
  if (!/^[A-Za-z0-9+/=]+$/.test(base64)) {
    return null;
  }
  const padding = base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((base64.length * 3) / 4) - padding;
  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > MAX_AUDIO_UPLOAD_BYTES) {
    return null;
  }

  return `data:${mediaType};base64,${base64}`;
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

const parseSuggestions = (input: unknown): Array<{ wrong: string; right: string; explanation: string }> => {
  if (!Array.isArray(input)) {
    return [];
  }

  return input
    .map((item) => {
      if (typeof item !== "object" || item === null) {
        return null;
      }

      const candidate = item as Record<string, unknown>;
      const wrong = typeof candidate.wrong === "string" ? candidate.wrong.trim() : "";
      const right = typeof candidate.right === "string" ? candidate.right.trim() : "";
      const explanation = typeof candidate.explanation === "string" ? candidate.explanation.trim() : "";

      if (!wrong || !right || !explanation) {
        return null;
      }

      return { wrong, right, explanation };
    })
    .filter((item): item is { wrong: string; right: string; explanation: string } => item !== null)
    .slice(0, 20);
};

export async function GET(request: NextRequest) {
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const [interestsResult, recordingsResult, subscription] = await Promise.all([
      query<InterestRow>(
        `SELECT interest_id
         FROM user_interests
         WHERE user_id = $1
         ORDER BY created_at ASC`,
        [user.id]
      ),
      query<RecordingRow>(
        `SELECT
           id,
           topic,
           duration,
           timestamp,
           transcript,
           suggestions,
           practice_type,
           audio_data_url,
           photo_data_url,
           photo_object
         FROM recordings
         WHERE user_id = $1
         ORDER BY timestamp DESC`,
        [user.id]
      ),
      getSubscriptionState(user.id)
    ]);
    const quota = await getRecordingQuota(user.id, { isSubscriber: subscription.isSubscriber });

    const interestIds = interestsResult.rows.map((row) => row.interest_id);
    const recordings = recordingsResult.rows.map((row) => ({
      id: row.id,
      topic: row.topic,
      duration: Math.max(0, Number(row.duration) || 0),
      timestamp: new Date(row.timestamp).toISOString(),
      transcript: row.transcript,
      suggestions: parseSuggestions(row.suggestions),
      practiceType: normalizePracticeType(row.practice_type),
      audioDataUrl: normalizeAudioDataUrl(row.audio_data_url),
      photoDataUrl: normalizePhotoDataUrl(row.photo_data_url),
      photoObject: normalizePhotoObject(row.photo_object)
    }));

    return NextResponse.json(
      {
        interestIds,
        recordings,
        quota,
        subscription,
        englishLevel: user.englishLevel
      },
      { status: 200 }
    );
  } catch (error) {
    console.error("User data route failed", error);
    return NextResponse.json({ error: "Failed to load user data." }, { status: 500 });
  }
}
