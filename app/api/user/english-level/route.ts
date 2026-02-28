import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { getUserEnglishLevel, saveUserEnglishLevel } from "../../../../src/server/englishLevel";

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
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const level = await getUserEnglishLevel(user.id);
    return NextResponse.json({ level }, { status: 200 });
  } catch (error) {
    console.error("User English level route failed", error);
    return NextResponse.json({ error: "Failed to load English level." }, { status: 500 });
  }
}

export async function PUT(request: NextRequest) {
  try {
    const user = await resolveAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const payload = (await request.json().catch(() => null)) as UpdateEnglishLevelPayload | null;
    const level = typeof payload?.level === "string" ? payload.level : "";
    const saved = await saveUserEnglishLevel(user.id, level);

    return NextResponse.json({ level: saved }, { status: 200 });
  } catch (error) {
    if (error instanceof Error && error.message === "English level is invalid.") {
      return NextResponse.json({ error: "English level is invalid." }, { status: 400 });
    }

    console.error("User English level update route failed", error);
    return NextResponse.json({ error: "Failed to save English level." }, { status: 500 });
  }
}
