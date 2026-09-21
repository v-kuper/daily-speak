import assert from "node:assert/strict";
import test from "node:test";

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
