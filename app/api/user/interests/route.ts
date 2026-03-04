import { NextResponse, type NextRequest } from "next/server";
import { query } from "../../../../src/server/db";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

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
  const logger = createRouteLogger("api.user.interests.put", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
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

    logger.info("request.success", {
      status: 200,
      durationMs: elapsedMs(startedAt),
      userId: user.id,
      interestsCount: interestIds.length
    });
    return NextResponse.json({ interestIds }, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to save interests." }, { status: 500 });
  }
}
