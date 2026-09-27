import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import ts from "typescript";

// Transpile the real TypeScript modules and share their module instances, including
// the configured client consumed by Redux. Only the external fetch is replaced.
const require = createRequire(import.meta.url);
const modules = new Map();
function importTypeScriptModule(file) {
  const filename = path.resolve(file);
  if (modules.has(filename)) return modules.get(filename).exports;
  const { outputText } = ts.transpileModule(readFileSync(filename, "utf8"), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  });
  const loadedModule = { exports: {} };
  modules.set(filename, loadedModule);
  const localRequire = (name) => name.startsWith(".")
    ? importTypeScriptModule(path.resolve(path.dirname(filename), `${name}.ts`))
    : require(name);
  new Function("require", "module", "exports", outputText)(localRequire, loadedModule, loadedModule.exports);
  return loadedModule.exports;
}

const apiConfig = importTypeScriptModule("src/lib/apiConfig.ts");
const apiClient = importTypeScriptModule("src/lib/apiClient.ts");
const mediaDownload = importTypeScriptModule("src/lib/mediaDownload.ts");
const webMiddleware = importTypeScriptModule("middleware.ts");

test("production requires an absolute HTTP API URL", () => {
  for (const value of [undefined, "", "   "]) {
    assert.throws(() => apiConfig.resolvePublicApiBaseUrl(value, "production"), /PUBLIC_API_BASE_URL/);
  }
  for (const value of ["/api", "//api.example.com", "javascript:alert(1)", "ftp://api.example.com"]) {
    assert.throws(() => apiConfig.resolvePublicApiBaseUrl(value, "production"), /http or https/);
  }
  assert.equal(apiConfig.resolvePublicApiBaseUrl("https://api.example.com/", "production"), "https://api.example.com");
  assert.equal(apiConfig.resolvePublicApiBaseUrl("http://localhost:3219///", "production"), "http://localhost:3219");
});

test("development defaults to the standalone API port", () => {
  assert.equal(apiConfig.resolvePublicApiBaseUrl(undefined, "development"), "http://localhost:3219");
});

test("LAN HTTP requests redirect to the canonical HTTPS web origin", () => {
  const redirect = apiConfig.resolveCanonicalWebRedirect?.({
    requestURL: "http://192.168.0.115:3218/speak?mode=free",
    host: "192.168.0.115:3218",
  }, "https://192.168.0.115:3443");

  assert.equal(redirect, "https://192.168.0.115:3443/speak?mode=free");
});

test("canonical web redirect trusts proxy origin and preserves internal health checks", () => {
  const resolveRedirect = apiConfig.resolveCanonicalWebRedirect;
  assert.equal(typeof resolveRedirect, "function");

  assert.equal(resolveRedirect({
    requestURL: "http://web:3000/history/recording-1?date=2026-09-22",
    host: "web:3000",
    forwardedHost: "192.168.0.115:3443",
    forwardedProto: "https",
  }, "https://192.168.0.115:3443"), null);

  assert.equal(resolveRedirect({
    requestURL: "http://127.0.0.1:3000/web-healthz",
    host: "127.0.0.1:3000",
  }, "https://192.168.0.115:3443"), null);

  assert.equal(resolveRedirect({
    requestURL: "http://localhost:3218/speak",
    host: "localhost:3218",
  }, undefined), null);
});

test("canonical web redirect keeps double-slash paths on the configured origin", () => {
  assert.equal(apiConfig.resolveCanonicalWebRedirect({
    requestURL: "http://192.168.0.115:3218//evil.example/path?q=1",
    host: "192.168.0.115:3218",
  }, "https://192.168.0.115:3443"), "https://192.168.0.115:3443//evil.example/path?q=1");
});

test("middleware enforces the runtime canonical web origin without breaking proxy or health traffic", (t) => {
  const { NextRequest } = require("next/server");
  const previous = process.env.PUBLIC_WEB_BASE_URL;
  t.after(() => {
    if (previous === undefined) delete process.env.PUBLIC_WEB_BASE_URL;
    else process.env.PUBLIC_WEB_BASE_URL = previous;
  });

  process.env.PUBLIC_WEB_BASE_URL = "https://192.168.0.115:3443";
  const redirect = webMiddleware.middleware(new NextRequest("http://192.168.0.115:3218/speak?mode=free"));
  assert.equal(redirect.status, 308);
  assert.equal(redirect.headers.get("location"), "https://192.168.0.115:3443/speak?mode=free");

  const proxyPass = webMiddleware.middleware(new NextRequest("http://web:3000/speak", { headers: {
    host: "web:3000",
    "x-forwarded-host": "192.168.0.115:3443",
    "x-forwarded-proto": "https",
  } }));
  assert.equal(proxyPass.status, 200);
  assert.equal(proxyPass.headers.get("location"), null);

  const healthPass = webMiddleware.middleware(new NextRequest("http://127.0.0.1:3000/web-healthz"));
  assert.equal(healthPass.status, 200);
  assert.equal(healthPass.headers.get("location"), null);

  delete process.env.PUBLIC_WEB_BASE_URL;
  const localPass = webMiddleware.middleware(new NextRequest("http://localhost:3218/speak"));
  assert.equal(localPass.status, 200);
  assert.equal(localPass.headers.get("location"), null);

  process.env.PUBLIC_WEB_BASE_URL = "https://192.168.0.115:3443/not-an-origin";
  assert.throws(
    () => webMiddleware.middleware(new NextRequest("http://192.168.0.115:3218/speak")),
    /PUBLIC_WEB_BASE_URL/,
  );
});

test("API requests preserve multipart chunks, blobs, JSON, options and response bodies", async () => {
  const controller = new AbortController();
  const form = new FormData();
  form.set("chunkIndex", "3");
  form.set("audio", new Blob(["audio"], { type: "audio/webm" }), "chunk-3.webm");
  for (const body of [form, new Blob(["audio"]), JSON.stringify({ topic: "Travel" })]) {
    const calls = [];
    const response = new Response("audio response");
    const client = apiClient.createApiClient("https://api.example.com/", async (url, init) => {
      calls.push({ url, init });
      return response;
    });
    const init = {
      method: "POST", body, signal: controller.signal, cache: "no-store",
      headers: new Headers({ "X-Test": "preserved" }), credentials: "omit", redirect: "error",
    };
    const result = await client.fetch("api/v1/media/uploads/demo/parts", init);
    assert.equal(calls[0].url, "https://api.example.com/api/v1/media/uploads/demo/parts");
    assert.deepEqual(calls[0].init, { ...init, credentials: "include" });
    assert.equal(calls[0].init.body, body);
    assert.equal(calls[0].init.signal, controller.signal);
    assert.equal(init.credentials, "omit");
    assert.equal(result, response);
    assert.equal(await result.text(), "audio response");
  }
});

test("network failures hide host details while abort errors keep their identity", async () => {
  const unavailable = apiClient.createApiClient("https://api.example.com", async () => {
    throw new TypeError("fetch failed: internal hostname details");
  });
  await assert.rejects(() => unavailable.fetch("/healthz"), (error) =>
    error instanceof apiClient.ApiUnavailableError && error.message === "The API is temporarily unavailable.");
  const abort = new DOMException("Cancelled", "AbortError");
  const cancelled = apiClient.createApiClient("https://api.example.com", async () => { throw abort; });
  await assert.rejects(() => cancelled.fetch("/api/topic-guidance"), (error) => error === abort);
});

test("shared API requests reject absolute URLs while opaque storage URLs can be resolved separately", async () => {
  const client = apiClient.createApiClient("https://api.example.com", async () => new Response(null));
  await assert.rejects(() => client.fetch("https://storage.example/upload"), /relative path/i);
  assert.equal(client.url("/signed-part?token=one"), "https://api.example.com/signed-part?token=one");
  assert.equal(client.url("https://storage.example/upload?token=two"), "https://storage.example/upload?token=two");
});

test("JSON parsing preserves valid payloads and empty bodies but rejects malformed bodies", async () => {
  assert.deepEqual(await apiClient.readApiJSON(new Response('{"error":"Validation failed"}', { status: 400 })), { error: "Validation failed" });
  assert.equal(await apiClient.readApiJSON(new Response(null, { status: 204 })), null);
  assert.equal(await apiClient.readApiJSON(new Response("")), null);
  await assert.rejects(() => apiClient.readApiJSON(new Response("not-json", { status: 502 })),
    { message: "The API returned an invalid response." });
});

test("the shared client requires initialization and uses the configured runtime origin", async (t) => {
  assert.throws(() => apiClient.apiFetch("/api/v1/auth/session"), /not configured/i);
  const calls = [];
  t.mock.method(globalThis, "fetch", async (url, init) => {
    calls.push({ url, init });
    return new Response(null, { status: 204 });
  });
  apiClient.configureApiClient("https://runtime.example");
  await apiClient.apiFetch("/api/v1/auth/session");
  assert.equal(calls[0].url, "https://runtime.example/api/v1/auth/session");
  assert.equal(calls[0].init.credentials, "include");
});

test("protected media playback exchanges an owned reference for a short-lived URL", async () => {
  apiClient.configureApiClient("https://api.example.com");
  const calls = [];
  const ticket = await mediaDownload.requestMediaPlaybackTicket({
    downloadPath: "/api/v1/media/asset-1/download",
    now: () => Date.parse("2026-09-27T10:00:00Z"),
    request: async (path, init) => {
      calls.push({ path, init });
      return Response.json({
        asset: { id: "asset-1", state: "ready" },
        request: {
          method: "GET",
          url: "/api/v1/media/local/assets/asset-1/content?expires=123&signature=secret",
          headers: {},
          expiresAt: "2026-09-27T10:15:00Z",
        },
      });
    },
  });

  assert.deepEqual(calls, [{
    path: "/api/v1/media/asset-1/download",
    init: { method: "GET", cache: "no-store", signal: undefined },
  }]);
  assert.equal(
    ticket.url,
    "https://api.example.com/api/v1/media/local/assets/asset-1/content?expires=123&signature=secret",
  );
  assert.equal(ticket.expiresAt, "2026-09-27T10:15:00.000Z");
});

test("protected media playback rejects mismatched, expired, and header-bound tickets", async () => {
  apiClient.configureApiClient("https://api.example.com");
  const downloadPath = "/api/v1/media/asset-1/download";
  const response = (overrides = {}) => Response.json({
    asset: { id: "asset-1", state: "ready" },
    request: {
      method: "GET",
      url: "https://storage.example/audio.mp3?signature=secret",
      headers: {},
      expiresAt: "2026-09-27T10:15:00Z",
    },
    ...overrides,
  });
  const now = () => Date.parse("2026-09-27T10:00:00Z");

  await assert.rejects(
    () => mediaDownload.requestMediaPlaybackTicket({
      downloadPath,
      now,
      request: async () => response({ asset: { id: "asset-2", state: "ready" } }),
    }),
    /invalid download request/i,
  );
  await assert.rejects(
    () => mediaDownload.requestMediaPlaybackTicket({
      downloadPath,
      now,
      request: async () => response({
        request: { method: "GET", url: "https://storage.example/audio.mp3", headers: {}, expiresAt: "2026-09-27T10:00:00Z" },
      }),
    }),
    /already expired/i,
  );
  await assert.rejects(
    () => mediaDownload.requestMediaPlaybackTicket({
      downloadPath,
      now,
      request: async () => response({
        request: {
          method: "GET",
          url: "https://storage.example/audio.mp3",
          headers: { Authorization: "secret" },
          expiresAt: "2026-09-27T10:15:00Z",
        },
      }),
    }),
    /cannot be played by the browser/i,
  );
});

test("recording requests use v1 media references and ignore legacy media URLs", async (t) => {
  const slice = importTypeScriptModule("src/store/slices/appSlice.ts");
  const { configureStore } = require("@reduxjs/toolkit");
  const store = configureStore({ reducer: { app: slice.default } });
  store.dispatch(slice.signIn.fulfilled({ email: "person@example.test", isSubscriber: false, englishLevel: "B1" }, "login"));
  const recording = {
    id: "recording-1", topic: "Travel", duration: 10, timestamp: "2026-09-21T10:00:00Z",
    status: "ready", transcript: "Hello", correctedTranscript: "Hello", suggestions: [],
    processingStage: null, practiceType: "photo_description", photoObject: "Tree", processingError: null,
    audioDataUrl: "/uploads/recordings/u/r.webm", photoDataUrl: "/uploads/photos/u/p.png",
    shadowingStatus: "ready", shadowingAudioUrl: "/uploads/shadowing/u/r.mp3",
    shadowingError: null, shadowingUpdatedAt: "2026-09-21T10:00:00Z",
    media: {
      audio: { assetId: "audio-1", downloadPath: "/api/v1/media/audio-1/download" },
      shadowing: { assetId: "shadowing-1", downloadPath: "/api/v1/media/shadowing-1/download" },
    },
  };
  let response = () => Response.json({ recording });
  t.mock.method(globalThis, "fetch", async (url, init) => {
    assert.equal(url, "https://api.example.com/api/v1/recordings/recording-1");
    assert.equal(init.credentials, "include");
    assert.equal(init.cache, "no-store");
    return response();
  });
  apiClient.configureApiClient("https://api.example.com");
  const parsed = await store.dispatch(slice.fetchRecording("recording-1")).unwrap();
  assert.equal(parsed.localAudioDataUrl, null);
  assert.equal(parsed.localPhotoDataUrl, null);
  assert.equal("shadowingAudioUrl" in parsed, false);
  assert.deepEqual(parsed.media, {
    audio: { assetId: "audio-1", downloadPath: "/api/v1/media/audio-1/download" },
    photo: null,
    shadowing: { assetId: "shadowing-1", downloadPath: "/api/v1/media/shadowing-1/download" },
  });
  response = () => Response.json({
    error: { code: "not_found", message: "Recording is unavailable.", requestId: "request-1" },
  }, { status: 404 });
  await assert.rejects(() => store.dispatch(slice.fetchRecording("recording-1")).unwrap(),
    (error) => error === "Recording is unavailable.");
});

test("application network calls use the shared API client", () => {
  for (const filename of ["src/store/slices/appSlice.ts", "src/components/SpeakScreen.tsx", "src/components/DetailsScreen.tsx"]) {
    const source = readFileSync(filename, "utf8");
    assert.doesNotMatch(source, /\bfetch\s*\(/, `${filename} bypasses apiFetch`);
    assert.doesNotMatch(source, /\.json\(\)\.catch/, `${filename} bypasses readApiJSON`);
  }
});
