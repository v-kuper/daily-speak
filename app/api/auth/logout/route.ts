import { NextResponse, type NextRequest } from "next/server";
import {
  SESSION_COOKIE_NAME,
  deleteSessionByToken,
  getSessionCookieOptions
} from "../../../../src/server/auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest) {
  try {
    const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
    await deleteSessionByToken(token);

    const response = NextResponse.json({ ok: true }, { status: 200 });
    response.cookies.set(SESSION_COOKIE_NAME, "", {
      ...getSessionCookieOptions(),
      maxAge: 0
    });
    return response;
  } catch (error) {
    console.error("Logout route failed", error);
    return NextResponse.json({ error: "Failed to sign out." }, { status: 500 });
  }
}
