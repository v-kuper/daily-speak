import assert from "node:assert/strict";
import test from "node:test";
import { configureStore } from "@reduxjs/toolkit";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const api = load("src/lib/apiClient.ts");
const guest = load("src/lib/guestPreview.ts");
const identityClient = load("src/lib/identity.ts");
const app = load("src/store/slices/appSlice.ts");
const flows = load("src/lib/routeFlows.ts");

const json = (body, status = 200, headers = {}) => new Response(JSON.stringify(body), {
  status,
  headers: { "Content-Type": "application/json", ...headers },
});

const tokens = (prefix = "guest") => ({
  tokenType: "Bearer",
  accessToken: `${prefix}-access`,
  accessTokenExpiresAt: "2099-01-01T00:15:00Z",
  refreshToken: `${prefix}-refresh`,
  refreshTokenExpiresAt: "2099-01-02T00:00:00Z",
});

const identity = (prefix = "guest") => ({
  principal: { id: `${prefix}-principal`, type: prefix === "guest" ? "guest" : "user" },
  session: { id: `${prefix}-session` },
  tokens: tokens(prefix),
});

const preview = (state = "queued") => ({
  id: "preview-123",
  state,
  topic: "Travel",
  duration: 3,
  timestamp: "2026-09-27T09:00:00Z",
  practiceType: "topic",
  transcript: state === "ready" ? "I go yesterday." : "",
  corrections: state === "ready" ? [{ wrong: "go", right: "went", explanation: "Use past tense." }] : [],
  expiresAt: "2099-01-02T00:00:00Z",
  createdAt: "2026-09-27T09:00:00Z",
  updatedAt: "2026-09-27T09:00:01Z",
});

const storage = () => {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
    removeItem: (key) => values.delete(key),
    clear: () => values.clear(),
  };
};

const router = () => {
  const visits = [];
  return {
    visits,
    push: (path) => visits.push(["push", path]),
    replace: (path) => visits.push(["replace", path]),
  };
};

function browser(t, handler) {
  identityClient.forgetBrowserIdentity();
  const sessionStorage = storage();
  const previousWindow = globalThis.window;
  globalThis.window = { sessionStorage };
  t.after(() => {
    identityClient.forgetBrowserIdentity();
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  });
  t.mock.method(globalThis, "fetch", handler);
  api.configureApiClient("https://api.example.test");
  return sessionStorage;
}

test("guest recording uses the v1 identity, multipart media contract, and opaque signed URL", async (t) => {
  const requests = [];
  browser(t, async (url, init = {}) => {
    requests.push({ url: String(url), init });
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/anonymous") return json(identity());
    if (path === "/api/v1/media/uploads") {
      const payload = JSON.parse(init.body);
      assert.equal(payload.purpose, "recording_audio");
      assert.equal(payload.sizeBytes, 3);
      assert.equal(payload.checksum.value, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
      assert.match(init.headers["Idempotency-Key"], /^web-upload:/);
      return json({
        asset: { id: "asset-123", state: "pending" },
        upload: { id: "upload-123", state: "pending", partSizeBytes: 3, partCount: 1, uploadedParts: [] },
      }, 201);
    }
    if (path === "/api/v1/media/uploads/upload-123/parts") {
      const part = JSON.parse(init.body).parts[0];
      assert.deepEqual(part, {
        partNumber: 1,
        sizeBytes: 3,
        checksumSha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
      });
      return json({ parts: [{ ...part, request: {
        method: "PUT",
        url: "https://storage.example.test/opaque-upload?signature=exact",
        headers: { "X-Signed-Header": "exact" },
      } }] });
    }
    if (String(url).startsWith("https://storage.example.test/opaque-upload")) {
      assert.equal(init.method, "PUT");
      assert.equal(init.headers["X-Signed-Header"], "exact");
      assert.equal(init.credentials, "omit");
      assert.equal(await init.body.text(), "abc");
      return new Response(null, { status: 200, headers: { ETag: "etag-part-1" } });
    }
    if (path === "/api/v1/media/uploads/upload-123/complete") {
      assert.deepEqual(JSON.parse(init.body).parts, [{
        partNumber: 1,
        etag: "etag-part-1",
        checksumSha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
      }]);
      return json({ asset: { id: "asset-123", state: "ready" }, upload: { id: "upload-123" } });
    }
    if (path === "/api/v1/guest/previews" && init.method === "POST") {
      assert.equal(init.headers.Authorization, "Bearer guest-access");
      assert.match(init.headers["Idempotency-Key"], /^web-preview:/);
      assert.deepEqual(JSON.parse(init.body), {
        audioAssetId: "asset-123",
        topic: "Travel",
        duration: 3,
        timestamp: "2026-09-27T09:00:00Z",
        practiceType: "topic",
      });
      return json({ preview: preview() }, 201);
    }
    throw new Error(`Unexpected request: ${url}`);
  });

  const result = await guest.createGuestPreview({
    topic: "Travel",
    duration: 3,
    timestamp: "2026-09-27T09:00:00Z",
    practiceType: "topic",
    audioDataUrl: "data:audio/webm;base64,YWJj",
  });
  assert.equal(result.id, "preview-123");
  assert.equal(guest.readGuestPreviewSession().previewId, "preview-123");
  assert.equal(guest.startNewGuestPreviewSession(), false);
  assert.equal(guest.readGuestPreviewSession().previewId, "preview-123");
  assert.ok(requests.some(({ url }) => url === "https://storage.example.test/opaque-upload?signature=exact"));
  assert.ok(requests.filter(({ url }) => url.startsWith("https://api.example.test")).every(({ init }) => init.credentials === "include"));
});

test("guest preview survives navigation, polls with its owner token, and limits corrections", async (t) => {
  let ready = false;
  browser(t, async (url, init = {}) => {
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/anonymous") return json(identity());
    if (path === "/api/v1/media/uploads") return json({
      asset: { id: "asset-123" }, upload: { id: "upload-123", partSizeBytes: 3, partCount: 1 },
    }, 201);
    if (path.endsWith("/parts")) return json({ parts: [{ partNumber: 1, request: { method: "PUT", url: "/signed-part", headers: {} } }] });
    if (path === "/signed-part") return new Response(null, { status: 200, headers: { ETag: "etag" } });
    if (path.endsWith("/complete")) return json({ asset: { id: "asset-123", state: "ready" } });
    if (path === "/api/v1/guest/previews" && init.method === "POST") return json({ preview: preview() }, 201);
    if (path === "/api/v1/guest/previews/preview-123") {
      assert.equal(init.headers.Authorization, "Bearer guest-access");
      return json({ preview: ready ? { ...preview("ready"), corrections: [
        { wrong: "one", right: "1", explanation: "First" },
        { wrong: "two", right: "2", explanation: "Second" },
        { wrong: "three", right: "3", explanation: "Hidden" },
      ] } : preview() });
    }
    throw new Error(`Unexpected request: ${url}`);
  });
  await guest.createGuestPreview({
    topic: "Travel", duration: 3, timestamp: "2026-09-27T09:00:00Z",
    practiceType: "topic", audioDataUrl: "data:audio/webm;base64,YWJj",
  });
  assert.equal((await guest.fetchGuestPreview("preview-123")).state, "queued");
  ready = true;
  const result = await guest.fetchGuestPreview("preview-123");
  assert.equal(result.state, "ready");
  assert.equal(result.corrections.length, 2);
});

for (const mode of ["signIn", "signUp"]) {
  test(`preview ${mode} promotes the stable ID through the unified browser identity`, async (t) => {
    const authPath = mode === "signIn" ? "/api/v1/auth/login" : "/api/v1/auth/register";
    browser(t, async (url, init = {}) => {
      const path = new URL(String(url)).pathname;
      if (path === "/api/v1/auth/anonymous") return json(identity());
      if (path === "/api/v1/media/uploads") return json({
        asset: { id: "asset-123" }, upload: { id: "upload-123", partSizeBytes: 3, partCount: 1 },
      }, 201);
      if (path.endsWith("/parts")) return json({ parts: [{ partNumber: 1, request: { method: "PUT", url: "/signed-part", headers: {} } }] });
      if (path === "/signed-part") return new Response(null, { status: 200, headers: { ETag: "etag" } });
      if (path.endsWith("/complete")) return json({ asset: { id: "asset-123", state: "ready" } });
      if (path === "/api/v1/guest/previews") return json({ preview: preview("ready") }, 201);
      if (path === authPath) {
        assert.equal(init.headers.Authorization, "Bearer guest-access");
        return json({
          ...identity("user"),
          user: { email: "person@example.test", isSubscriber: false, englishLevel: "b1" },
          guestPreviewPromotion: { status: "promoted", previewId: "preview-123", recordingId: "preview-123" },
        }, mode === "signIn" ? 200 : 201);
      }
      throw new Error(`Unexpected request: ${url}`);
    });
    await guest.createGuestPreview({
      topic: "Travel", duration: 3, timestamp: "2026-09-27T09:00:00Z",
      practiceType: "topic", audioDataUrl: "data:audio/webm;base64,YWJj",
    });

    const initial = app.default(undefined, { type: "test/init" });
    const store = configureStore({
      reducer: { app: app.default },
      preloadedState: { app: {
        ...initial,
        authInitialized: true,
        authEmailDraft: "person@example.test",
        authPasswordDraft: "password123",
      } },
    });
    const navigation = router();
    await flows.authenticateAndNavigate(store, navigation, mode, "/preview/preview-123");
    assert.equal(store.getState().app.isAuthenticated, true);
    assert.equal(store.getState().app.speakState, "idle");
    assert.equal(store.getState().app.pendingRecordingAudioDataUrl, null);
    assert.deepEqual(navigation.visits, [["replace", "/history/preview-123"]]);
    assert.equal(guest.readGuestPreviewSession(), null);
  });
}

test("a lost upload-create response retries with the same idempotency key", async (t) => {
  let uploadCreates = 0;
  const uploadKeys = [];
  browser(t, async (url, init = {}) => {
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/anonymous") return json(identity());
    if (path === "/api/v1/media/uploads") {
      uploadCreates += 1;
      uploadKeys.push(init.headers["Idempotency-Key"]);
      if (uploadCreates === 1) throw new Error("response lost after server commit");
      return json({ asset: { id: "asset-123", state: "uploading" }, upload: {
        id: "upload-123", state: "uploading", partSizeBytes: 3, partCount: 1,
      } }, 200);
    }
    if (path.endsWith("/parts")) return json({ parts: [{ partNumber: 1, request: { method: "PUT", url: "/signed-part", headers: {} } }] });
    if (path === "/signed-part") return new Response(null, { status: 200, headers: { ETag: "etag" } });
    if (path.endsWith("/complete")) return json({ asset: { id: "asset-123", state: "ready" } });
    if (path === "/api/v1/guest/previews") return json({ preview: preview() }, 201);
    throw new Error(`Unexpected request: ${url}`);
  });
  const draft = {
    topic: "Travel", duration: 3, timestamp: "2026-09-27T09:00:00Z",
    practiceType: "topic", audioDataUrl: "data:audio/webm;base64,YWJj",
  };
  await assert.rejects(() => guest.createGuestPreview(draft));
  assert.equal((await guest.createGuestPreview(draft)).id, "preview-123");
  assert.equal(uploadCreates, 2);
  assert.equal(uploadKeys[0], uploadKeys[1]);
});

test("a transient refresh failure preserves only non-secret guest preview metadata", async (t) => {
  const sessionStorage = browser(t, async (url) => {
    if (new URL(String(url)).pathname === "/api/v1/auth/refresh") throw new Error("temporarily offline");
    throw new Error(`Unexpected request: ${url}`);
  });
  sessionStorage.setItem("daily-speaking.guest-preview.v1", JSON.stringify({
    principalId: "guest-principal",
    previewId: "preview-123",
  }));
  await assert.rejects(() => guest.fetchGuestPreview("preview-123"));
  assert.deepEqual(guest.readGuestPreviewSession(), {
    principalId: "guest-principal",
    previewId: "preview-123",
  });
  assert.doesNotMatch(sessionStorage.getItem("daily-speaking.guest-preview.v1"), /refreshToken|accessToken/);
});

test("a declined promotion keeps the account active and explains why the preview was not saved", async (t) => {
  const sessionStorage = browser(t, async (url, init = {}) => {
    const path = new URL(String(url)).pathname;
    if (path === "/api/v1/auth/refresh") return json(identity());
    if (path === "/api/v1/auth/login") return json({
      ...identity("user"),
      user: { email: "person@example.test", isSubscriber: false, englishLevel: "b1" },
      guestPreviewPromotion: { status: "not_promoted", previewId: "preview-123", reason: "quota_exceeded" },
    });
    throw new Error(`Unexpected request: ${url}`);
  });
  sessionStorage.setItem("daily-speaking.guest-preview.v1", JSON.stringify({
    principalId: "guest-principal", previewId: "preview-123",
  }));
  const initial = app.default(undefined, { type: "test/init" });
  const store = configureStore({ reducer: { app: app.default }, preloadedState: { app: {
    ...initial, authInitialized: true, authEmailDraft: "person@example.test", authPasswordDraft: "password123",
  } } });
  const navigation = router();
  await flows.authenticateAndNavigate(store, navigation, "signIn", "/preview/preview-123");
  assert.equal(store.getState().app.isAuthenticated, true);
  assert.match(store.getState().app.recordingInputError, /quota/i);
  assert.deepEqual(navigation.visits, [["replace", "/speak"]]);
});

test("guest preview rejects unsupported and overlong drafts before spending backend resources", async (t) => {
  let requests = 0;
  browser(t, async () => { requests += 1; throw new Error("network should not be reached"); });
  await assert.rejects(() => guest.createGuestPreview({
    topic: "Photo", duration: 10, timestamp: "2026-09-27T09:00:00Z",
    practiceType: "photo_description", audioDataUrl: "data:audio/webm;base64,YWJj",
  }), /require an account/i);
  await assert.rejects(() => guest.createGuestPreview({
    topic: "Long", duration: 61, timestamp: "2026-09-27T09:00:00Z",
    practiceType: "topic", audioDataUrl: "data:audio/webm;base64,YWJj",
  }), /between 1 and 60 seconds/i);
  assert.equal(requests, 0);
});

test("guest recording stops at the backend preview limit", () => {
  let state = app.default(undefined, { type: "test/init" });
  state = app.default(state, app.startFreeTalk());
  for (let second = 0; second < 65; second += 1) state = app.default(state, app.tickRecording());
  assert.equal(state.recordingDuration, 60);
  assert.equal(state.speakState, "recorded");
});
