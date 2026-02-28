import { NextResponse, type NextRequest } from "next/server";
import {
  SESSION_COOKIE_NAME,
  getSessionCookieOptions,
  getUserBySessionToken
} from "../../../../src/server/auth";

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
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    const user = await getUserBySessionToken(token);

    if (!user) {
      return unauthorizedResponse();
    }

    return NextResponse.json({ user: { email: user.email, isSubscriber: user.isSubscriber } }, { status: 200 });
  } catch (error) {
    console.error("Session route failed", error);
    return NextResponse.json({ error: "Failed to load session." }, { status: 500 });
  }
}
