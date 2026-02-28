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

type RecordingRow = {
  id: string;
  topic: string;
  duration: number;
  timestamp: string;
  transcript: string;
  suggestions: unknown;
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
        `SELECT id, topic, duration, timestamp, transcript, suggestions
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
      suggestions: parseSuggestions(row.suggestions)
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
