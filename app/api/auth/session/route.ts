import { NextResponse, type NextRequest } from "next/server";
import {
  SESSION_COOKIE_NAME,
  getSessionCookieOptions,
  getUserBySessionToken
} from "../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const unauthorizedResponse = (): NextResponse => {
  const response = NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  response.cookies.set(SESSION_COOKIE_NAME, "", {
    ...getSessionCookieOptions(),
    maxAge: 0
  });
  return response;
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.auth.session", request);
  const startedAt = Date.now();
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      logger.info("request.unauthorized", { status: 401, durationMs: elapsedMs(startedAt) });
      return unauthorizedResponse();
    }

    logger.info("request.success", { status: 200, durationMs: elapsedMs(startedAt), userId: user.id });
    return NextResponse.json(
      { user: { email: user.email, isSubscriber: user.isSubscriber, englishLevel: user.englishLevel } },
      { status: 200 }
    );
  } catch (error) {
    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to load session." }, { status: 500 });
  }
}
