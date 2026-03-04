import { NextResponse, type NextRequest } from "next/server";
import {
  AuthError,
  SESSION_COOKIE_NAME,
  createSession,
  getSessionCookieOptions,
  loginUser,
  validateCredentials
} from "../../../../src/server/auth";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../../src/server/logger";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type CredentialsPayload = {
  email?: unknown;
  password?: unknown;
};

const parseCredentials = (payload: CredentialsPayload | null): { email: string; password: string } => {
  const rawEmail = typeof payload?.email === "string" ? payload.email : "";
  const rawPassword = typeof payload?.password === "string" ? payload.password : "";
  return validateCredentials(rawEmail, rawPassword);
};

export async function POST(request: NextRequest) {
  const logger = createRouteLogger("api.auth.login", request);
  const startedAt = Date.now();
  try {
    const payload = (await request.json().catch(() => null)) as CredentialsPayload | null;
    const { email, password } = parseCredentials(payload);
    const user = await loginUser(email, password);
    const session = await createSession(user.id);

    const response = NextResponse.json(
      { user: { email: user.email, isSubscriber: user.isSubscriber, englishLevel: user.englishLevel } },
      { status: 200 }
    );
    response.cookies.set(SESSION_COOKIE_NAME, session.token, getSessionCookieOptions(session.expiresAt));
    logger.info("request.success", { status: 200, durationMs: elapsedMs(startedAt), userId: user.id });
    return response;
  } catch (error) {
    if (error instanceof AuthError) {
      logger.warn("request.rejected", { status: error.status, durationMs: elapsedMs(startedAt), reason: error.message });
      return NextResponse.json({ error: error.message }, { status: error.status });
    }

    logger.error("request.failed", { status: 500, durationMs: elapsedMs(startedAt), ...toErrorMeta(error) });
    return NextResponse.json({ error: "Failed to sign in." }, { status: 500 });
  }
}
