import { randomUUID } from "node:crypto";
import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import {
  FREE_WEEKLY_LIMIT_SECONDS,
  getRecordingQuota,
  SUBSCRIBER_MAX_SESSION_SECONDS
} from "../../../../src/server/recordingQuota";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434";
const DEFAULT_OLLAMA_MODEL = "gemma3:12b";

type RecordingSuggestion = {
  wrong: string;
  right: string;
  explanation: string;
};

type RecordingAnalysis = {
  transcript: string;
  suggestions: RecordingSuggestion[];
};

type CreateRecordingPayload = {
  recording?: {
    topic?: unknown;
    duration?: unknown;
    timestamp?: unknown;
  };
};

type RecordingRow = {
  id: string;
  topic: string;
  duration: number;
  timestamp: string;
  transcript: string;
  suggestions: unknown;
};

type InterestRow = {
  interest_id: string;
};

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
};

type ParsedRecordingPayload = {
  transcript?: unknown;
  suggestions?: unknown;
  corrections?: unknown;
  mistakes?: unknown;
  errorAnalysis?: unknown;
};

class RecordingAnalysisError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

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

const parseRecordingAnalysis = (content: string): RecordingAnalysis | null => {
  const normalizePayload = (data: ParsedRecordingPayload | null): RecordingAnalysis | null => {
    if (!data) {
      return null;
    }

    const transcript = normalizeTranscript(data.transcript);
    const suggestions = normalizeSuggestions(data.suggestions ?? data.corrections ?? data.mistakes ?? data.errorAnalysis);

    if (!transcript || suggestions.length === 0) {
      return null;
    }

    return {
      transcript,
      suggestions: suggestions.slice(0, 5)
    };
  };

  const direct = normalizePayload(tryParseJson(content));
  if (direct) {
    return direct;
  }

  const jsonCandidate = content.match(/\{[\s\S]*\}/);
  if (!jsonCandidate) {
    return null;
  }

  return normalizePayload(tryParseJson(jsonCandidate[0]));
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

const createPrompt = (topic: string, duration: number, interests: string[]): string => {
  const clampedDuration = Math.max(30, Math.min(duration, 600));
  const targetWords = Math.max(80, Math.min(320, Math.round(clampedDuration * 2.1)));

  const parts = [
    `Topic: \"${topic}\".`,
    `Recording duration: about ${Math.max(1, duration)} seconds.`,
    `Write one plausible English learner transcript around ${targetWords} words (B1-B2 level).`,
    "Then provide exactly 4 grammar-focused corrections based on this transcript.",
    'Return only JSON with this shape: {"transcript":"...","suggestions":[{"wrong":"...","right":"...","explanation":"..."}]}.',
    "No markdown, no extra keys, no explanations outside JSON."
  ];

  if (interests.length > 0) {
    parts.push(`Learner interests: ${interests.join(", ")}. Keep transcript context close to them when possible.`);
  }

  return parts.join(" ");
};

const buildRequestBody = (
  model: string,
  prompt: string,
  seed: number,
  strictJson: boolean
): Record<string, unknown> => {
  return {
    model,
    stream: false,
    format: "json",
    messages: [
      {
        role: "system",
        content: strictJson
          ? "Return strict valid JSON only. No markdown. No prose."
          : "You generate realistic speaking-practice transcript mocks and grammar correction items. Follow output JSON format exactly."
      },
      {
        role: "user",
        content: prompt
      }
    ],
    options: {
      temperature: strictJson ? 0.3 : 0.6,
      seed
    }
  };
};

const generateRecordingAnalysis = async (
  topic: string,
  duration: number,
  interests: string[]
): Promise<RecordingAnalysis> => {
  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const model = process.env.OLLAMA_MODEL ?? DEFAULT_OLLAMA_MODEL;
  const seed = Math.abs((hashString(topic.toLowerCase()) * 131 + duration * 17 + Date.now()) % 2_147_483_647);
  const prompt = createPrompt(topic, duration, interests);

  for (let attempt = 0; attempt < 2; attempt += 1) {
    const strictJson = attempt > 0;
    let response: Response;

    try {
      response = await fetch(`${baseUrl}/api/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(buildRequestBody(model, prompt, seed + attempt * 97, strictJson)),
        cache: "no-store"
      });
    } catch {
      throw new RecordingAnalysisError(
        "Cannot connect to local Ollama. Make sure Ollama is running for transcript generation.",
        502
      );
    }

    if (!response.ok) {
      const text = await response.text();
      throw new RecordingAnalysisError(`Ollama request failed (${response.status}): ${text.slice(0, 300)}`, 502);
    }

    const payload = (await response.json()) as OllamaChatResponse;
    const content = payload.message?.content?.trim();
    if (!content) {
      continue;
    }

    const analysis = parseRecordingAnalysis(content);
    if (analysis) {
      return analysis;
    }
  }

  throw new RecordingAnalysisError("Could not parse transcript and suggestions from Ollama response.", 502);
};

export async function POST(request: NextRequest) {
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as CreateRecordingPayload | null;
    const source = payload?.recording;

    const topic = typeof source?.topic === "string" ? source.topic.trim() : "";
    const duration = Number.parseInt(String(source?.duration ?? 0), 10);

    if (!topic) {
      return NextResponse.json({ error: "Recording topic is required." }, { status: 400 });
    }

    if (!Number.isFinite(duration) || duration < 0) {
      return NextResponse.json({ error: "Recording duration is invalid." }, { status: 400 });
    }

    const quotaBefore = await getRecordingQuota(user.id, { isSubscriber: user.isSubscriber });
    if (quotaBefore.isSubscriber) {
      if (duration > SUBSCRIBER_MAX_SESSION_SECONDS) {
        return NextResponse.json(
          { error: "Subscribers can save recordings up to 10:00 per session." },
          { status: 400 }
        );
      }
    } else {
      const remaining = quotaBefore.weeklyRemainingSeconds ?? 0;
      if (duration > remaining) {
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

    const interestsResult = await query<InterestRow>(
      `SELECT interest_id
       FROM user_interests
       WHERE user_id = $1
       ORDER BY created_at ASC`,
      [user.id]
    );
    const interests = interestsResult.rows.map((row) => row.interest_id).slice(0, 10);

    const analysis = await generateRecordingAnalysis(topic, duration, interests);

    const insertResult = await query<RecordingRow>(
      `INSERT INTO recordings (id, user_id, topic, duration, timestamp, transcript, suggestions)
       VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
       RETURNING id, topic, duration, timestamp, transcript, suggestions`,
      [
        id,
        user.id,
        topic.slice(0, 300),
        duration,
        timestamp.toISOString(),
        analysis.transcript,
        JSON.stringify(analysis.suggestions)
      ]
    );

    const row = insertResult.rows[0];
    const recording = {
      id: row.id,
      topic: row.topic,
      duration: Math.max(0, Number(row.duration) || 0),
      timestamp: new Date(row.timestamp).toISOString(),
      transcript: row.transcript,
      suggestions: normalizeSuggestions(row.suggestions)
    };
    const quota = await getRecordingQuota(user.id, { isSubscriber: user.isSubscriber });

    return NextResponse.json({ recording, quota }, { status: 201 });
  } catch (error) {
    if (error instanceof RecordingAnalysisError) {
      return NextResponse.json({ error: error.message }, { status: error.status });
    }

    console.error("User recordings route failed", error);
    return NextResponse.json({ error: "Failed to save recording." }, { status: 500 });
  }
}
