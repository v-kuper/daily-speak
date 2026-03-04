import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { getUserEnglishLevel, saveUserEnglishLevel } from "../../../../src/server/englishLevel";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type UpdateEnglishLevelPayload = {
  level?: unknown;
};

const resolveAuthorizedUser = async (request: NextRequest) => {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  return getUserBySessionToken(token);
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.user.english-level.get", request);
  const startedAt = Date.now();
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const level = await getUserEnglishLevel(user.id);
    logger.info("request.success", { status: 200, durationMs: elapsedMs(startedAt), userId: user.id, level });
    return NextResponse.json({ level }, { status: 200 });
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load English level." }, { status: 500 });
  }
}

export async function PUT(request: NextRequest) {
  const logger = createRouteLogger("api.user.english-level.put", request);
  const startedAt = Date.now();
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as UpdateEnglishLevelPayload | null;
    const level = typeof payload?.level === "string" ? payload.level : "";
    const saved = await saveUserEnglishLevel(user.id, level);

    logger.info("request.success", { status: 200, durationMs: elapsedMs(startedAt), userId: user.id, level: saved });
    return NextResponse.json({ level: saved }, { status: 200 });
  } catch (error) {
    if (error instanceof Error && error.message === "English level is invalid.") {
      logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: error.message });
      return NextResponse.json({ error: "English level is invalid." }, { status: 400 });
    }

    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to save English level." }, { status: 500 });
  }
}
