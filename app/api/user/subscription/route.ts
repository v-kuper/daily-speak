import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../../src/server/auth";
import { query } from "../../../../src/server/db";
import { getRecordingQuota } from "../../../../src/server/recordingQuota";
import { getSubscriptionState } from "../../../../src/server/subscription";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const getAuthorizedUser = async (request: NextRequest) => {
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  return getUserBySessionToken(token);
};

export async function GET(request: NextRequest) {
  try {
    const user = await getAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const subscription = await getSubscriptionState(user.id);
    const quota = await getRecordingQuota(user.id, { isSubscriber: subscription.isSubscriber });
    return NextResponse.json({ subscription, quota }, { status: 200 });
  } catch (error) {
    console.error("User subscription route failed", error);
    return NextResponse.json({ error: "Failed to load subscription." }, { status: 500 });
  }
}

export async function POST(request: NextRequest) {
  try {
    const user = await getAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    await query(
      `UPDATE users
       SET
         is_subscriber = TRUE,
         subscription_cancelled = FALSE,
         subscription_expires_at = CASE
           WHEN subscription_expires_at IS NOT NULL AND subscription_expires_at > NOW()
             THEN subscription_expires_at + INTERVAL '1 month'
           ELSE NOW() + INTERVAL '1 month'
         END
       WHERE id = $1`,
      [user.id]
    );

    const subscription = await getSubscriptionState(user.id);
    const quota = await getRecordingQuota(user.id, { isSubscriber: subscription.isSubscriber });
    return NextResponse.json({ subscription, quota }, { status: 200 });
  } catch (error) {
    console.error("User subscription create route failed", error);
    return NextResponse.json({ error: "Failed to activate subscription." }, { status: 500 });
  }
}

export async function DELETE(request: NextRequest) {
  try {
    const user = await getAuthorizedUser(request);
    if (!user) {
      return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
    }

    const cancelResult = await query(
      `UPDATE users
       SET
         subscription_cancelled = TRUE,
         subscription_expires_at = COALESCE(subscription_expires_at, NOW() + INTERVAL '1 month')
       WHERE id = $1
         AND is_subscriber = TRUE
         AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())`,
      [user.id]
    );

    if ((cancelResult.rowCount ?? 0) === 0) {
      return NextResponse.json({ error: "No active subscription to cancel." }, { status: 400 });
    }

    const subscription = await getSubscriptionState(user.id);
    const quota = await getRecordingQuota(user.id, { isSubscriber: subscription.isSubscriber });
    return NextResponse.json({ subscription, quota }, { status: 200 });
  } catch (error) {
    console.error("User subscription cancel route failed", error);
    return NextResponse.json({ error: "Failed to cancel subscription." }, { status: 500 });
  }
}
