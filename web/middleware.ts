import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";
import { resolveCanonicalWebRedirect } from "./src/lib/apiConfig";

export function middleware(request: NextRequest) {
  const destination = resolveCanonicalWebRedirect({
    requestURL: request.url,
    host: request.headers.get("host"),
    forwardedHost: request.headers.get("x-forwarded-host"),
    forwardedProto: request.headers.get("x-forwarded-proto"),
  }, process.env.PUBLIC_WEB_BASE_URL);

  return destination
    ? NextResponse.redirect(destination, 308)
    : NextResponse.next();
}
