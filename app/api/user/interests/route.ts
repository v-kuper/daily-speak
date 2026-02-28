import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const MAX_INTERESTS = 10;

type InterestsPayload = {
  interestIds?: unknown;
};

const normalizeInterests = (value: unknown): string[] => {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const item of value) {
    if (typeof item !== "string") {
      continue;
    }

    const cleaned = item.trim();
    if (!cleaned || cleaned.length > 80) {
      continue;
    }

    const key = cleaned.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    normalized.push(cleaned);

    if (normalized.length >= MAX_INTERESTS) {
      break;
    }
  }

  return normalized;
};

export async function PUT(request: NextRequest) {
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as InterestsPayload | null;
    const interestIds = normalizeInterests(payload?.interestIds);

    await query("DELETE FROM user_interests WHERE user_id = $1", [user.id]);

    if (interestIds.length > 0) {
      await query(
        `INSERT INTO user_interests (user_id, interest_id)
         SELECT $1, interest_id
         FROM UNNEST($2::text[]) AS t(interest_id)
         ON CONFLICT DO NOTHING`,
        [user.id, interestIds]
      );
    }

    return NextResponse.json({ interestIds }, { status: 200 });
  } catch (error) {
    console.error("User interests route failed", error);
    return NextResponse.json({ error: "Failed to save interests." }, { status: 500 });
  }
}
