import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const routes = createTypeScriptLoader()("src/lib/routes.ts");

test("safeReturnTo accepts only known internal routes", () => {
  for (const path of ["/speak", "/history", "/history?date=2026-09-21", "/history/recording-123", "/profile", "/profile/subscription", "/profile/english-level", "/profile/interests"]) {
    assert.equal(routes.safeReturnTo(path), path);
  }
  for (const value of [undefined, null, 42, "", "/", "/auth", "https://evil.example", "//evil.example", "/feed", "/history/../profile", "/history/%", "/history/%2e%2e", "/history/%2F%2Fevil.example", "/history/recording/extra", "/history\\evil", "/history#fragment", "/history?date=2026-02-30", "/history?date=2026-09-21&next=https://evil.example", "/history?date=2026-09-21&date=2026-09-22", "/history?date=2026-09-21?", "/speak?date=2026-09-21", "javascript:alert(1)"]) {
    assert.equal(routes.safeReturnTo(value), "/speak", String(value));
  }
});

test("history date accepts only real UTC YYYY-MM-DD dates", () => {
  for (const value of ["2026-09-21", "2024-02-29", "2000-02-29", "0099-01-01"]) {
    assert.equal(routes.parseHistoryDate(value), value);
  }
  for (const value of [undefined, null, 42, "2026-02-30", "2026-02-29", "1900-02-29", "2026-04-31", "2026-00-01", "2026-13-01", "2026-01-00", "21-09-2026", "2026-9-21", "2026-09-21T00:00:00Z", "2026-09-21\n"]) {
    assert.equal(routes.parseHistoryDate(value), null, String(value));
  }
});

test("return paths reject trailing control characters and extra query segments", () => {
  for (const path of ["/history/recording-123\n", "/history?date=2026-09-21&", "/history?&date=2026-09-21"]) {
    assert.equal(routes.safeReturnTo(path), "/speak");
  }
});

test("recording paths encode IDs as one path component", () => {
  assert.equal(routes.recordingPath("recording-123"), "/history/recording-123");
  assert.equal(routes.recordingPath("id /?#%"), "/history/id%20%2F%3F%23%25");
});

test("protected routes wait for session restoration and preserve only safe destinations", () => {
  assert.equal(routes.protectedRouteDestination(false, false, "/history"), null);
  assert.equal(routes.protectedRouteDestination(false, true, "/history"), null);
  assert.equal(routes.protectedRouteDestination(true, true, "/history"), null);
  assert.equal(routes.protectedRouteDestination(true, false, "/history/recording-123"), "/auth?returnTo=%2Fhistory%2Frecording-123");
  assert.equal(routes.protectedRouteDestination(true, false, "/history?date=2026-09-21"), "/auth?returnTo=%2Fhistory%3Fdate%3D2026-09-21");
  assert.equal(routes.protectedRouteDestination(true, false, "//evil.example"), "/auth?returnTo=%2Fspeak");
});

test("App Router owns every supported screen (boundary backstop)", () => {
  for (const path of ["speak", "auth", "history", "history/[recordingId]", "profile", "profile/subscription", "profile/english-level", "profile/interests"]) {
    assert.equal(existsSync(`app/${path}/page.tsx`), true, `missing ${path}`);
  }
  const appSlice = readFileSync("src/store/slices/appSlice.ts", "utf8");
  assert.doesNotMatch(appSlice, /currentScreen|activeTab|navigateToTab|screenBeforeAuth/);
});
