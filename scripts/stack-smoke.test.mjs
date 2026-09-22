import assert from "node:assert/strict";
import test from "node:test";

import * as stackSmoke from "./smoke-stack.mjs";
import {
  extractCookieHeader,
  resolveApiUploadURL,
  selectStackURLs,
} from "./smoke-stack.mjs";

test("stack smoke selects independent default and configured service URLs", () => {
  assert.deepEqual(selectStackURLs({}), {
    webBaseURL: "http://localhost:3218",
    apiBaseURL: "http://localhost:3219",
    webOrigin: "http://localhost:3218",
  });

  assert.deepEqual(selectStackURLs({
    WEB_BASE_URL: " https://web.example.test/app/ ",
    API_BASE_URL: " https://api.example.test/v1/ ",
  }), {
    webBaseURL: "https://web.example.test/app",
    apiBaseURL: "https://api.example.test/v1",
    webOrigin: "https://web.example.test",
  });
});

test("stack smoke resolves backend-owned upload URLs against the API origin", () => {
  assert.equal(
    resolveApiUploadURL("/uploads/recordings/user/recording.webm", "https://api.example.test/base"),
    "https://api.example.test/uploads/recordings/user/recording.webm",
  );
  assert.equal(
    resolveApiUploadURL("https://api.example.test/uploads/recording.webm", "https://api.example.test/base"),
    "https://api.example.test/uploads/recording.webm",
  );
  assert.throws(
    () => resolveApiUploadURL("https://cdn.example.test/recording.webm", "https://api.example.test"),
    /API origin/,
  );
});

test("stack smoke extracts request cookies without retaining Set-Cookie attributes", () => {
  const headers = {
    getSetCookie: () => [
      "daily_speaking_session=secret; Path=/; HttpOnly; SameSite=Lax",
      "preference=compact; Path=/; Max-Age=3600",
    ],
  };

  assert.equal(
    extractCookieHeader(headers),
    "daily_speaking_session=secret; preference=compact",
  );
  assert.equal(extractCookieHeader({ get: () => null }), "");
});

test("readiness timeout reports the last sanitized endpoint failure without leaking request data", async () => {
  assert.equal(typeof stackSmoke.waitForServices, "function");

  let now = 0;
  let apiAttempts = 0;
  const fetchImpl = async (url) => {
    if (url.includes("/web-healthz")) return { ok: true, status: 200 };
    apiAttempts += 1;
    if (apiAttempts === 1) throw new Error("superseded connection refusal");

    const cause = new Error([
      "certificate verify failed for https://alice:password-secret@api.example.test/healthz?token=query-secret",
      "Authorization: Bearer authorization-secret",
      "Cookie: session=cookie-secret",
      "Set-Cookie: session=set-cookie-secret",
      "token=token-secret api_key=api-key-secret",
      "Response body: response-body-secret",
      "Body: plain-body-secret",
      "Headers: response-header-secret",
      "Cookie jar: cookie-jar-secret",
      "x".repeat(500),
    ].join("\n"));
    throw new TypeError("fetch failed", { cause });
  };

  let failure;
  try {
    await stackSmoke.waitForServices(
      {
        webBaseURL: "https://web.example.test:3443",
        apiBaseURL: "https://api.example.test:3444",
      },
      fetchImpl,
      {
        startupTimeoutMs: 15,
        pollIntervalMs: 10,
        now: () => now,
        sleep: async (milliseconds) => { now += milliseconds; },
      },
    );
  } catch (error) {
    failure = error;
  }

  assert.ok(failure instanceof Error);
  assert.match(failure.message, /^Services did not become ready: API\./);
  assert.match(failure.message, /API: TypeError: fetch failed; caused by Error: certificate verify failed/);
  assert.match(failure.message, /https:\/\/api\.example\.test\/healthz\?\[redacted\]/);
  assert.match(failure.message, /Authorization=\[redacted\]/);
  assert.match(failure.message, /Cookie=\[redacted\]/);
  assert.match(failure.message, /Set-Cookie=\[redacted\]/);
  assert.match(failure.message, /token=\[redacted\]/);
  assert.match(failure.message, /api_key=\[redacted\]/);
  assert.doesNotMatch(failure.message, /superseded connection refusal/);
  for (const secret of [
    "alice", "password-secret", "query-secret", "authorization-secret",
    "cookie-secret", "set-cookie-secret", "token-secret", "api-key-secret",
    "response-body-secret", "plain-body-secret", "response-header-secret", "cookie-jar-secret",
  ]) {
    assert.doesNotMatch(failure.message, new RegExp(secret));
  }
  assert.doesNotMatch(failure.message, /[\r\n]/);
  assert.ok(failure.message.length <= 340, `failure was not bounded: ${failure.message.length}`);
});

test("readiness timeout keeps the generic endpoint message when requests return without errors", async () => {
  assert.equal(typeof stackSmoke.waitForServices, "function");

  let now = 0;
  await assert.rejects(
    stackSmoke.waitForServices(
      {
        webBaseURL: "https://web.example.test:3443",
        apiBaseURL: "https://api.example.test:3444",
      },
      async () => ({ ok: false, status: 503 }),
      {
        startupTimeoutMs: 1,
        pollIntervalMs: 1,
        now: () => now,
        sleep: async (milliseconds) => { now += milliseconds; },
      },
    ),
    { message: "Services did not become ready: web, API." },
  );
});

test("readiness diagnostics redact response metadata even when it is embedded in an error message", async () => {
  let now = 0;
  const fetchImpl = async (url) => {
    if (url.includes("/healthz") && !url.includes("/web-healthz")) {
      return { ok: true, status: 200 };
    }
    throw new Error([
      "Body: plain-body-secret",
      "Response body: response-body-secret",
      "Headers: response-header-secret",
      "Cookie jar: cookie-jar-secret",
    ].join("\n"));
  };

  let failure;
  try {
    await stackSmoke.waitForServices(
      {
        webBaseURL: "https://web.example.test:3443",
        apiBaseURL: "https://api.example.test:3444",
      },
      fetchImpl,
      {
        startupTimeoutMs: 1,
        pollIntervalMs: 1,
        now: () => now,
        sleep: async (milliseconds) => { now += milliseconds; },
      },
    );
  } catch (error) {
    failure = error;
  }

  assert.ok(failure instanceof Error);
  assert.match(failure.message, /^Services did not become ready: web\./);
  assert.match(failure.message, /Body=\[redacted\]/);
  assert.match(failure.message, /Response body=\[redacted\]/);
  assert.match(failure.message, /Headers=\[redacted\]/);
  assert.match(failure.message, /Cookie jar=\[redacted\]/);
  assert.doesNotMatch(failure.message, /plain-body-secret|response-body-secret|response-header-secret|cookie-jar-secret/);
});
