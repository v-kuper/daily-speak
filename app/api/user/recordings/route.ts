import { randomUUID } from "node:crypto";
import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RecordingSuggestion = {
  wrong: string;
  right: string;
  explanation: string;
};

type CreateRecordingPayload = {
  recording?: {
    topic?: unknown;
    duration?: unknown;
    timestamp?: unknown;
    transcript?: unknown;
    suggestions?: unknown;
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
      const wrong = typeof candidate.wrong === "string" ? candidate.wrong.trim() : "";
      const right = typeof candidate.right === "string" ? candidate.right.trim() : "";
      const explanation = typeof candidate.explanation === "string" ? candidate.explanation.trim() : "";

      if (!wrong || !right || !explanation) {
        return null;
      }

      return { wrong, right, explanation };
    })
    .filter((item): item is RecordingSuggestion => item !== null)
    .slice(0, 20);
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
    const transcript = typeof source?.transcript === "string" ? source.transcript.trim() : "";
    const duration = Number.parseInt(String(source?.duration ?? 0), 10);
    const suggestions = normalizeSuggestions(source?.suggestions);

    if (!topic) {
      return NextResponse.json({ error: "Recording topic is required." }, { status: 400 });
    }

    if (!transcript) {
      return NextResponse.json({ error: "Recording transcript is required." }, { status: 400 });
    }

    if (!Number.isFinite(duration) || duration < 0) {
      return NextResponse.json({ error: "Recording duration is invalid." }, { status: 400 });
    }

    const timestamp = parseTimestamp(source?.timestamp);
    const id = randomUUID();

    const insertResult = await query<RecordingRow>(
      `INSERT INTO recordings (id, user_id, topic, duration, timestamp, transcript, suggestions)
       VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
       RETURNING id, topic, duration, timestamp, transcript, suggestions`,
      [id, user.id, topic.slice(0, 300), duration, timestamp.toISOString(), transcript.slice(0, 20000), JSON.stringify(suggestions)]
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

    return NextResponse.json({ recording }, { status: 201 });
  } catch (error) {
    console.error("User recordings route failed", error);
    return NextResponse.json({ error: "Failed to save recording." }, { status: 500 });
  }
}
