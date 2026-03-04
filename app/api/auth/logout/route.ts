import { NextResponse, type NextRequest } from "next/server";
import {
  SESSION_COOKIE_NAME,
  deleteSessionByToken,
  getSessionCookieOptions
} from "../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest) {
  const logger = createRouteLogger("api.auth.logout", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    await deleteSessionByToken(token);

    const response = NextResponse.json({ ok: true }, { status: 200 });
    response.cookies.set(SESSION_COOKIE_NAME, "", {
      ...getSessionCookieOptions(),
      maxAge: 0
    });
    logger.info("request.success", { status: 200, durationMs: elapsedMs(startedAt) });
    return response;
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to sign out." }, { status: 500 });
  }
}
