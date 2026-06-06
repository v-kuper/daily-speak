import { randomUUID } from "node:crypto";
import { mkdir, unlink, writeFile } from "node:fs/promises";
import path from "node:path";
import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import {
  FREE_WEEKLY_LIMIT_SECONDS,
  getRecordingQuota,
  SUBSCRIBER_MAX_SESSION_SECONDS
} from "../../../../src/server/recordingQuota";
import {
  DEFAULT_OLLAMA_BASE_URL,
  extractOllamaMessageContent,
  extractJsonCandidates,
  getOllamaThinkOption,
  resolveOllamaSettingsForUser
} from "../../../../src/server/ollama";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";
import { WhisperTranscriptionError, transcribeAudioWithLocalWhisper } from "../../../../src/server/whisper";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RecordingSuggestion = {
  wrong: string;
  right: string;
  explanation: string;
};

type PracticeType = "free_talk" | "topic" | "photo_description";

type CreateRecordingPayload = {
  recording?: {
    topic?: unknown;
    duration?: unknown;
    timestamp?: unknown;
    practiceType?: unknown;
    audioDataUrl?: unknown;
    photoDataUrl?: unknown;
    photoObject?: unknown;
  };
};

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

type InterestRow = {
  interest_id: string;
};

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
  response?: string;
};

type ParsedRecordingPayload = {
  suggestions?: unknown;
  corrections?: unknown;
  mistakes?: unknown;
  errorAnalysis?: unknown;
};

const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);
const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
const AUDIO_FILE_URL_PATTERN = /^\/uploads\/recordings\/[a-z0-9/_-]+\.[a-z0-9]{2,10}$/i;
const AUDIO_PUBLIC_BASE_URL = "/uploads/recordings";
const AUDIO_STORAGE_ROOT_DIR = path.join(process.cwd(), "public", "uploads", "recordings");
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
const MAX_AUDIO_UPLOAD_BYTES = 80 * 1024 * 1024;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;
const MAX_PHOTO_UPLOAD_BYTES = 4 * 1024 * 1024;

type ParsedIncomingAudioDataUrl = {
  normalizedDataUrl: string;
  base64: string;
  extension: string;
};

type SavedAudioFile = {
  publicUrl: string;
  absolutePath: string;
};

const hashString = (value: string): number => {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash);
};

const formatSeconds = (seconds: number): string => {
  const normalized = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(normalized / 60);
  const restSeconds = normalized % 60;
  return `${minutes}:${restSeconds.toString().padStart(2, "0")}`;
};

const normalizeSuggestions = (input: unknown): RecordingSuggestion[] => {
  if (!Array.isArray(input)) {
    return [];
  }

  return input
    .map((item) => {
      if (typeof item !== "object" || item === null) {
        return null;
      }

      const candidate = item as Record<string, unknown>;
      const wrongRaw = candidate.wrong ?? candidate.original ?? candidate.mistake ?? candidate.incorrect;
      const rightRaw = candidate.right ?? candidate.correct ?? candidate.correction ?? candidate.fixed;
      const explanationRaw = candidate.explanation ?? candidate.reason ?? candidate.note ?? candidate.comment;
      const wrong = typeof wrongRaw === "string" ? wrongRaw.trim() : "";
      const right = typeof rightRaw === "string" ? rightRaw.trim() : "";
      const explanation = typeof explanationRaw === "string" ? explanationRaw.trim() : "";

      if (!wrong || !right || !explanation) {
        return null;
      }

      return { wrong, right, explanation };
    })
    .filter((item): item is RecordingSuggestion => item !== null)
    .slice(0, 8);
};

const normalizeTranscript = (value: unknown): string => {
  if (typeof value !== "string") {
    return "";
  }

  return value
    .trim()
    .replace(/\s+/g, " ")
    .slice(0, 20000);
};

const tryParseJson = (jsonText: string): ParsedRecordingPayload | null => {
  try {
    return JSON.parse(jsonText) as ParsedRecordingPayload;
  } catch {
    return null;
  }
};

const parseSuggestionsFromContent = (content: string): RecordingSuggestion[] => {
  for (const candidate of extractJsonCandidates(content)) {
    const parsed = tryParseJson(candidate);
    if (!parsed) {
      continue;
    }

    const suggestions = normalizeSuggestions(parsed.suggestions ?? parsed.corrections ?? parsed.mistakes ?? parsed.errorAnalysis);
    if (suggestions.length > 0) {
      return suggestions.slice(0, 5);
    }
  }

  return [];
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

const normalizePracticeType = (value: unknown): PracticeType => {
  if (typeof value !== "string") {
    return "topic";
  }

  const normalized = value.trim().toLowerCase() as PracticeType;
  return PRACTICE_TYPE_SET.has(normalized) ? normalized : "topic";
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

const normalizeStoredAudioSource = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  if (AUDIO_FILE_URL_PATTERN.test(normalized)) {
    return normalized;
  }

  const parsed = parseIncomingAudioDataUrl(normalized);
  return parsed ? parsed.normalizedDataUrl : null;
};

const sanitizePathSegment = (value: string): string => {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, "_").slice(0, 80);
  return normalized || "user";
};

const saveAudioFile = async (userId: string, recordingId: string, audio: ParsedIncomingAudioDataUrl): Promise<SavedAudioFile> => {
  const userDir = sanitizePathSegment(userId);
  const fileName = `${recordingId}.${audio.extension}`;
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

const normalizePhotoObject = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().replace(/\s+/g, " ").slice(0, 120);
  return normalized || null;
};

const normalizePhotoDataUrl = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  const match = normalized.match(PHOTO_DATA_URL_PATTERN);
  if (!match) {
    return null;
  }

  const mimeRaw = match[1].toLowerCase();
  const mime = mimeRaw === "jpg" ? "jpeg" : mimeRaw;
  const base64 = match[2].trim();
  const padding = base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((base64.length * 3) / 4) - padding;

  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > MAX_PHOTO_UPLOAD_BYTES) {
    return null;
  }

  return `data:image/${mime};base64,${base64}`;
};

const createSuggestionsPrompt = (
  transcript: string,
  topic: string,
  interests: string[],
  practiceType: PracticeType,
  photoObject: string | null
): string => {
  const normalizedTranscript = normalizeTranscript(transcript);
  const transcriptForPrompt = normalizedTranscript.slice(0, 6000);

  const parts = [
    `Topic: \"${topic}\".`,
    "You receive an English learner transcript from a speaking practice recording.",
    "Find up to 4 grammar or word-choice mistakes that clearly appear in the transcript.",
    'Return only JSON with this exact shape: {"suggestions":[{"wrong":"...","right":"...","explanation":"..."}]}.',
    "Do not invent mistakes that are not present in the transcript.",
    "No markdown and no extra keys.",
    `Transcript: """${transcriptForPrompt}""".`
  ];

  if (practiceType === "photo_description") {
    parts.push("Practice mode: photo description.");
    if (photoObject) {
      parts.push(`Main photo object: \"${photoObject}\".`);
    }
  } else if (practiceType === "free_talk") {
    parts.push("Practice mode: free talk.");
  }

  if (interests.length > 0) {
    parts.push(`Learner interests context: ${interests.join(", ")}.`);
  }

  return parts.join(" ");
};

const buildRequestBody = (
  model: string,
  isThinkingModel: boolean,
  prompt: string,
  seed: number,
  strictJson: boolean,
  useJsonFormat: boolean
): Record<string, unknown> => {
  const body: Record<string, unknown> = {
    model,
    stream: false,
    think: getOllamaThinkOption(isThinkingModel),
    messages: [
      {
        role: "system",
        content: strictJson
          ? "Return strict valid JSON only. No markdown. No prose."
          : "You analyze learner transcripts and output only grammar correction JSON."
      },
      {
        role: "user",
        content: prompt
      }
    ],
    options: {
      temperature: strictJson ? 0.1 : 0.3,
      seed
    }
  };

  if (useJsonFormat) {
    body.format = "json";
  }

  return body;
};

const generateRecordingSuggestions = async (
  userId: string,
  transcript: string,
  topic: string,
  interests: string[],
  practiceType: PracticeType,
  photoObject: string | null,
  logger?: ReturnType<typeof createRouteLogger>
): Promise<RecordingSuggestion[]> => {
  if (!transcript) {
    return [];
  }

  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const ollamaSettings = await resolveOllamaSettingsForUser();
  const { model, isThinkingModel } = ollamaSettings;
  const useJsonFormat = !isThinkingModel;
  const seed = Math.abs((hashString(topic.toLowerCase()) * 131 + hashString(transcript) * 17) % 2_147_483_647);
  const prompt = createSuggestionsPrompt(transcript, topic, interests, practiceType, photoObject);

  for (let attempt = 0; attempt < 2; attempt += 1) {
    const strictJson = attempt > 0;
    let response: Response;

    try {
      response = await fetch(`${baseUrl}/api/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(buildRequestBody(model, isThinkingModel, prompt, seed + attempt * 97, strictJson, useJsonFormat)),
        cache: "no-store"
      });
    } catch (error) {
      logger?.warn("ollama.suggestions_request_failed", { model, attempt: attempt + 1, ...toErrorMeta(error) });
      return [];
    }

    if (!response.ok) {
      logger?.warn("ollama.suggestions_request_failed", { model, attempt: attempt + 1, status: response.status });
      return [];
    }

    let payload: OllamaChatResponse;
    try {
      payload = (await response.json()) as OllamaChatResponse;
    } catch (error) {
      logger?.warn("ollama.suggestions_invalid_json", { model, attempt: attempt + 1, ...toErrorMeta(error) });
      return [];
    }
    const content = extractOllamaMessageContent(payload);
    if (!content) {
      logger?.debug("ollama.suggestions_empty_content", { model, attempt: attempt + 1 });
      continue;
    }

    const suggestions = parseSuggestionsFromContent(content);
    if (suggestions.length > 0) {
      logger?.debug("ollama.suggestions_success", { model, attempt: attempt + 1, suggestionsCount: suggestions.length });
      return suggestions;
    }
    logger?.debug("ollama.suggestions_parse_failed", { model, attempt: attempt + 1, contentLength: content.length });
  }

  return [];
};

export async function POST(request: NextRequest) {
  const logger = createRouteLogger("api.user.recordings.post", request);
  const startedAt = Date.now();
  let savedAudioAbsolutePath: string | null = null;

  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as CreateRecordingPayload | null;
    const source = payload?.recording;

    const practiceType = normalizePracticeType(source?.practiceType);
    let topic = typeof source?.topic === "string" ? source.topic.trim() : "";
    const duration = Number.parseInt(String(source?.duration ?? 0), 10);
    const rawAudioDataUrl = source?.audioDataUrl;
    const parsedAudioDataUrl = parseIncomingAudioDataUrl(rawAudioDataUrl);
    const rawPhotoDataUrl = source?.photoDataUrl;
    const photoDataUrl = normalizePhotoDataUrl(rawPhotoDataUrl);
    const photoObject = normalizePhotoObject(source?.photoObject);

    logger.info("request.start", {
      userId: user.id,
      practiceType,
      duration,
      hasAudioDataUrl: typeof rawAudioDataUrl === "string",
      hasPhotoDataUrl: typeof rawPhotoDataUrl === "string",
      hasPhotoObject: Boolean(photoObject)
    });

    if (typeof rawAudioDataUrl === "string" && !parsedAudioDataUrl) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_audio_payload" });
      return NextResponse.json(
        { error: `Audio must be a valid recording under ${Math.floor(MAX_AUDIO_UPLOAD_BYTES / (1024 * 1024))}MB.` },
        { status: 400 }
      );
    }

    if (!parsedAudioDataUrl) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_audio" });
      return NextResponse.json({ error: "Audio recording is required." }, { status: 400 });
    }

    if (typeof rawPhotoDataUrl === "string" && !photoDataUrl) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_photo_payload" });
      return NextResponse.json(
        { error: `Photo must be a valid image under ${Math.floor(MAX_PHOTO_UPLOAD_BYTES / (1024 * 1024))}MB.` },
        { status: 400 }
      );
    }

    if (practiceType === "photo_description") {
      if (!photoDataUrl) {
        logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_photo" });
        return NextResponse.json({ error: "Photo is required for photo description practice." }, { status: 400 });
      }
      if (!topic) {
        topic = "Photo description";
      }
    }

    if (!topic) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_topic" });
      return NextResponse.json({ error: "Recording topic is required." }, { status: 400 });
    }

    if (!Number.isFinite(duration) || duration < 0) {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_duration" });
      return NextResponse.json({ error: "Recording duration is invalid." }, { status: 400 });
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

    const timestamp = parseTimestamp(source?.timestamp);
    const id = randomUUID();
    const savedAudio = await saveAudioFile(user.id, id, parsedAudioDataUrl);
    savedAudioAbsolutePath = savedAudio.absolutePath;
    logger.info("audio.saved", { userId: user.id, recordingId: id, audioUrl: savedAudio.publicUrl });

    const interestsResult = await query<InterestRow>(
      `SELECT interest_id
       FROM user_interests
       WHERE user_id = $1
       ORDER BY created_at ASC`,
      [user.id]
    );
    const interests = interestsResult.rows.map((row) => row.interest_id).slice(0, 10);

    const transcript = normalizeTranscript(await transcribeAudioWithLocalWhisper(savedAudio.absolutePath));
    if (!transcript) {
      throw new WhisperTranscriptionError(
        "Whisper returned an empty transcript. Try speaking louder or recording again.",
        422
      );
    }
    logger.info("transcription.success", { userId: user.id, recordingId: id, transcriptLength: transcript.length });

    const suggestions = await generateRecordingSuggestions(user.id, transcript, topic, interests, practiceType, photoObject, logger);

    const insertResult = await query<RecordingRow>(
      `INSERT INTO recordings
         (id, user_id, topic, duration, timestamp, transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object)
       VALUES
         ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
       RETURNING
         id,
         topic,
         duration,
         timestamp,
         transcript,
         suggestions,
         practice_type,
         audio_data_url,
         photo_data_url,
         photo_object`,
      [
        id,
        user.id,
        topic.slice(0, 300),
        duration,
        timestamp.toISOString(),
        transcript,
        JSON.stringify(suggestions),
        practiceType,
        savedAudio.publicUrl,
        practiceType === "photo_description" ? photoDataUrl : null,
        practiceType === "photo_description" ? photoObject : null
      ]
    );
    savedAudioAbsolutePath = null;

    const row = insertResult.rows[0];
    const recording = {
      id: row.id,
      topic: row.topic,
      duration: Math.max(0, Number(row.duration) || 0),
      timestamp: new Date(row.timestamp).toISOString(),
      transcript: row.transcript,
      suggestions: normalizeSuggestions(row.suggestions),
      practiceType: normalizePracticeType(row.practice_type),
      audioDataUrl: normalizeStoredAudioSource(row.audio_data_url),
      photoDataUrl: normalizePhotoDataUrl(row.photo_data_url),
      photoObject: normalizePhotoObject(row.photo_object)
    };
    const quota = await getRecordingQuota(user.id, { isSubscriber: user.isSubscriber });

    logger.info("request.success", {
      status: 201,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      recordingId: recording.id,
      suggestionsCount: recording.suggestions.length
    });

    return NextResponse.json({ recording, quota }, { status: 201 });
  } catch (error) {
    if (savedAudioAbsolutePath) {
      await unlink(savedAudioAbsolutePath).catch(() => undefined);
      logger.warn("audio.cleanup", { durationMs: elapsedMs(startedAt), removedPath: savedAudioAbsolutePath });
    }

    if (error instanceof WhisperTranscriptionError) {
      logger.warn("request.rejected", {
        status: error.status,
        durationMs: elapsedMs(startedAt),
        reason: error.message
      });
      return NextResponse.json({ error: error.message }, { status: error.status });
    }

    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to save recording." }, { status: 500 });
  }
}
