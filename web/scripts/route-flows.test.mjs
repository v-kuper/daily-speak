import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";
import { configureStore } from "@reduxjs/toolkit";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Provider } from "react-redux";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const app = load("src/store/slices/appSlice.ts");
const api = load("src/lib/apiClient.ts");
// Keep missing behavior an assertion failure during RED, rather than a module-load error.
const flows = existsSync("src/lib/routeFlows.ts") ? load("src/lib/routeFlows.ts") : {};
const initial = () => app.default(undefined, { type: "test/init" });
const storeFor = (overrides = {}) => configureStore({
  reducer: { app: app.default }, preloadedState: { app: { ...initial(), ...overrides } },
});
const draft = {
  localRecordingId: "local-123", recordingUploadSessionId: "upload-123", topic: "Travel",
  duration: 20, timestamp: "2026-09-21T10:00:00Z", practiceType: "topic",
  audioDataUrl: "data:audio/webm;base64,YWJj", photoDataUrl: null, photoObject: null,
};
const saved = {
  id: "permanent-123", topic: "Travel", duration: 20, timestamp: draft.timestamp,
  practiceType: "topic", status: "processing", transcript: "", correctedTranscript: "",
  suggestions: [], processingStage: "transcription", audioDataUrl: "/uploads/saved.webm",
  photoDataUrl: null, photoObject: null, processingError: null, shadowingStatus: "pending",
  shadowingAudioUrl: null, shadowingError: null, shadowingUpdatedAt: draft.timestamp,
};
const guest = {
  authInitialized: true, authEmailDraft: "person@example.test", authPasswordDraft: "password123",
  speakState: "recorded", selectedTopic: "Travel", recordingDuration: 20,
  pendingRecordingAudioDataUrl: draft.audioDataUrl, pendingSaveAfterAuth: true,
};
const routerFor = () => {
  const visits = [];
  return { visits, push: (path) => visits.push(["push", path]), replace: (path) => visits.push(["replace", path]) };
};
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const response = (body, status = 200) => new Response(JSON.stringify(body), { status });
function server(t, handler) {
  // Only the network is replaced: real API encoding, thunks, reducers, and route flow run.
  t.mock.method(globalThis, "fetch", handler);
  api.configureApiClient("https://api.example.test");
}
function flow(name) {
  assert.equal(typeof flows[name], "function", `${name} must implement the route transition`);
  return flows[name];
}

for (const mode of ["signIn", "signUp"]) {
  test(`${mode} preserves a guest recording through authentication, saves once, then replaces the route`, async (t) => {
    const run = flow("authenticateAndNavigate");
    const store = storeFor(guest), router = routerFor(), pending = deferred(), requests = [];
    server(t, async (url, init) => {
      requests.push([url, JSON.parse(init.body)]);
      if (url.endsWith(mode === "signIn" ? "/login" : "/register")) {
        return pending.promise;
      }
      assert.equal(url, "https://api.example.test/api/user/recordings");
      assert.equal(JSON.parse(init.body).recording.audioDataUrl, draft.audioDataUrl);
      return response({ recording: saved });
    });
    const attempt = run(store, router, mode, "/profile");
    await run(store, router, mode, "/profile");
    assert.deepEqual(router.visits, []);
    pending.resolve(response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } }));
    await attempt;
    assert.equal(requests.length, 2);
    assert.deepEqual(router.visits, [["replace", "/history/permanent-123"]]);
    assert.equal(store.getState().app.pendingSaveAfterAuth, false);
    assert.equal(store.getState().app.pendingRecordingAudioDataUrl, null);
  });
}

test("auth returnTo is validated and rejected sign-in stays with a visible Redux error", async (t) => {
  const run = flow("authenticateAndNavigate");
  server(t, async () => response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } }));
  for (const [returnTo, expected] of [["/profile/english-level", "/profile/english-level"], ["//evil.example", "/speak"]]) {
    const store = storeFor({ ...guest, pendingSaveAfterAuth: false }), router = routerFor();
    await run(store, router, "signIn", returnTo);
    assert.deepEqual(router.visits, [["replace", expected]]);
  }
  const store = storeFor({ ...guest, authPasswordDraft: "bad" }), router = routerFor();
  await run(store, router, "signIn", "/profile");
  assert.deepEqual(router.visits, []);
  assert.match(store.getState().app.authError, /Password/);
});

test("rejected post-auth save remains on auth with a visible error and a retryable guest draft", async (t) => {
  const run = flow("authenticateAndNavigate");
  const store = storeFor(guest), router = routerFor();
  let saveCalls = 0;
  server(t, async (url) => url.endsWith("/login")
    ? response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } })
    : (++saveCalls, response({ error: "Storage unavailable" }, 503)));
  await run(store, router, "signIn", "/profile");
  assert.deepEqual(router.visits, []);
  assert.equal(saveCalls, 1);
  assert.equal(store.getState().app.recordingSaveError, "Storage unavailable");
  assert.equal(store.getState().app.pendingRecordingAudioDataUrl, draft.audioDataUrl);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
});

test("guest save retains the recording and cancel clears both auth drafts and returns to speaking", () => {
  const store = storeFor({ ...guest, pendingSaveAfterAuth: false }), router = routerFor();
  flow("startGuestSave")(store, router);
  assert.deepEqual(router.visits, [["push", "/auth?returnTo=%2Fspeak"]]);
  assert.equal(store.getState().app.pendingRecordingAudioDataUrl, draft.audioDataUrl);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
  flow("cancelAuthentication")(store, router);
  assert.equal(store.getState().app.authEmailDraft, "");
  assert.equal(store.getState().app.authPasswordDraft, "");
  assert.equal(store.getState().app.pendingSaveAfterAuth, false);
  assert.deepEqual(router.visits.at(-1), ["replace", "/speak"]);
});

test("authenticated save immediately opens the local recording, waits for final upload, then replaces the ID", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  const upload = deferred(), save = deferred();
  let saves = 0;
  server(t, async (url) => {
    assert.equal(url, "https://api.example.test/api/recording-sessions/upload-123/finish");
    saves++;
    return save.promise;
  });
  const attempt = run(store, router, draft, upload.promise);
  assert.deepEqual(router.visits, [["push", "/history/local-123"]]);
  assert.equal(store.getState().app.recordings[0].id, "local-123");
  assert.equal(saves, 0);
  upload.resolve();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(saves, 1);
  assert.equal(router.visits.length, 1);
  save.resolve(response({ recording: saved }));
  await attempt;
  assert.deepEqual(router.visits.at(-1), ["replace", "/history/permanent-123"]);
  assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["permanent-123"]);
});

for (const failure of ["upload", "primary save"]) {
  test(`${failure} failure falls back to data URL and removes the optimistic recording on success`, async (t) => {
    const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
    const requests = [];
    server(t, async (url, init) => {
      requests.push(url);
      if (url.endsWith("/finish")) return response({ error: "Session expired" }, 500);
      assert.equal(url, "https://api.example.test/api/user/recordings");
      assert.equal(JSON.parse(init.body).recording.audioDataUrl, draft.audioDataUrl);
      return response({ recording: saved });
    });
    await run(store, router, draft, failure === "upload" ? Promise.reject(new Error("upload failed")) : null);
    assert.equal(requests.length, failure === "upload" ? 1 : 2);
    assert.deepEqual(router.visits, [["push", "/history/local-123"], ["replace", "/history/permanent-123"]]);
    assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["permanent-123"]);
    assert.equal(store.getState().app.recordingSaveError, null);
  });
}

test("terminal save failure returns to history and retains a visible failed recording", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  server(t, async () => response({ error: "Storage unavailable" }, 503));
  await run(store, router, draft, null);
  assert.deepEqual(router.visits, [["push", "/history/local-123"], ["replace", "/history"]]);
  assert.equal(store.getState().app.recordingSaveError, "Storage unavailable");
  assert.equal(store.getState().app.recordings[0].status, "failed");
  assert.equal(store.getState().app.backgroundSaveRecordingId, null);
});

test("a history refresh keeps a pending or failed local recording visible", () => {
  const store = storeFor({ isAuthenticated: true });
  store.dispatch(app.showBackgroundRecordingSave(draft));
  const refresh = () => store.dispatch(app.fetchUserData.fulfilled({
    recordings: [], interestIds: [], englishLevel: "B1", quota: null, subscription: null,
  }, "refresh"));
  refresh();
  assert.equal(store.getState().app.backgroundSaveRecordingId, "local-123");
  assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["local-123"]);
  store.dispatch(app.saveRecording.rejected(null, "save", draft, "Storage unavailable"));
  refresh();
  assert.equal(store.getState().app.recordings[0].status, "failed");
  assert.equal(store.getState().app.recordingSaveError, "Storage unavailable");
});

test("fallback remains a background save while the fallback response is pending", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  const fallback = deferred();
  server(t, async (url) => url.endsWith("/finish")
    ? response({ error: "Session unavailable" }, 503) : fallback.promise);
  const attempt = run(store, router, draft, null);
  await new Promise((resolve) => setImmediate(resolve));
  const duringFallback = store.getState().app;
  // Resolve before asserting to avoid leaving this test's network promise pending on RED.
  fallback.resolve(response({ recording: saved }));
  await attempt;
  assert.equal(duringFallback.backgroundSaveRecordingId, "local-123");
  assert.equal(duringFallback.recordings[0].status, "processing");
});

test("history date is absent from Redux and validated from the route query", () => {
  assert.equal("selectedDate" in initial(), false);
  const historyDate = flow("historyDateFromSearch");
  assert.equal(historyDate(new URLSearchParams("date=2026-09-21")), "2026-09-21");
  for (const search of ["", "date=2026-02-30", "date=2026-09-21&date=2026-09-22"]) {
    assert.equal(historyDate(new URLSearchParams(search)), null);
  }
});

test("route selection uses the requested ID and never requests local or deleted IDs", () => {
  const select = flow("recordingDetailState");
  const state = { ...initial(), isAuthenticated: true, recordings: [saved], currentRecordingId: "stale-id" };
  assert.equal(select(state, "permanent-123").recording.id, "permanent-123");
  assert.equal(select(state, "permanent-123").shouldFetch, false);
  assert.equal(select(state, "unloaded").shouldFetch, true);
  assert.equal(select(state, "local-123").shouldFetch, false);
  assert.match(select(state, "local-123").error, /not found/i);
  assert.equal(select({ ...state, deletedRecordingIds: ["deleted"] }, "deleted").shouldFetch, false);
});

for (const status of [401, 403, 404]) {
  test(`detail ${status} response stays stable without retrying or trusting the route ID`, async (t) => {
    const select = flow("recordingDetailState"), store = storeFor({ isAuthenticated: true });
    server(t, async (url, init) => {
      assert.equal(url, "https://api.example.test/api/recordings/other%20owner");
      assert.equal(init.credentials, "include");
      return response({ error: "Recording not accessible" }, status);
    });
    await store.dispatch(app.fetchRecording("other owner"));
    assert.equal(select(store.getState().app, "other owner").shouldFetch, false);
    assert.ok(select(store.getState().app, "other owner").error);
    assert.equal(store.getState().app.isAuthenticated, true);
  });
}

test("deletion waits for success before leaving details and stays on rejection", async (t) => {
  const run = flow("deleteAndNavigate"), store = storeFor({ isAuthenticated: true, recordings: [saved] }), router = routerFor();
  const pending = deferred();
  server(t, async () => pending.promise);
  const attempt = run(store, router, "permanent-123");
  assert.deepEqual(router.visits, []);
  pending.resolve(response({ deletedRecordingId: "permanent-123" }));
  await attempt;
  assert.deepEqual(router.visits, [["replace", "/history"]]);
  assert.deepEqual(store.getState().app.recordings, []);
  server(t, async () => response({ error: "Cannot delete" }, 503));
  await run(store, router, "another-id");
  assert.equal(router.visits.length, 1);
  assert.equal(store.getState().app.recordingDeleteError, "Cannot delete");
});

test("profile route sections render their settings and back links directly", () => {
  const ProfileScreen = load("src/components/ProfileScreen.tsx").default;
  const store = storeFor({ isAuthenticated: true });
  const render = (section) => renderToStaticMarkup(createElement(Provider, { store }, createElement(ProfileScreen, { section })));
  const home = render("home");
  assert.match(home, /href="\/profile\/subscription"/);
  assert.match(home, /href="\/profile\/english-level"/);
  const subscription = render("subscription");
  assert.match(subscription, /План и подписка/);
  assert.match(subscription, /href="\/profile"/);
  assert.doesNotMatch(subscription, /<select/);
  const english = render("english-level");
  assert.match(english, /<select/);
  assert.match(english, /href="\/profile"/);
  assert.doesNotMatch(english, /Оформить на месяц/);
});

test("Next detail page awaits its decoded param and passes the ID without double-decoding", async () => {
  const Page = load("app/history/[recordingId]/page.tsx").default;
  const pending = deferred();
  const page = Page({ params: pending.promise });
  pending.resolve({ recordingId: "recording %2F / ?" });
  const element = await page;
  assert.equal(element.props.children.props.recordingId, "recording %2F / ?");
  assert.equal(element.props.returnTo, "/history/recording%20%252F%20%2F%20%3F");
});

test("route screens consume the exercised flows and literal profile routes (wiring backstop)", () => {
  const read = (path) => readFileSync(path, "utf8");
  assert.match(read("src/components/AuthScreen.tsx"), /authenticateAndNavigate/);
  assert.match(read("src/components/AuthScreen.tsx"), /recordingSaveError/);
  assert.match(read("src/components/SpeakScreen.tsx"), /saveAndNavigate/);
  assert.doesNotMatch(read("src/components/AppShell.tsx"), /dispatch\(saveRecording/);
  assert.match(read("src/components/HistoryScreen.tsx"), /historyDateFromSearch\(searchParams\)/);
  assert.match(read("src/components/HistoryScreen.tsx"), /recordingSaveError/);
  assert.match(read("src/components/HistoryScreen.tsx"), /recordingPath\(recording\.id\)/);
  assert.match(read("src/components/DetailsScreen.tsx"), /recordingDetailState/);
  assert.match(read("src/components/DetailsScreen.tsx"), /deleteAndNavigate/);
  assert.match(read("app/history/[recordingId]/page.tsx"), /await params/);
  assert.match(read("app/history/[recordingId]/page.tsx"), /DetailsScreen recordingId=\{recordingId\}/);
  for (const [path, section] of [["profile", "home"], ["profile/subscription", "subscription"], ["profile/english-level", "english-level"]]) {
    assert.ok(read(`app/${path}/page.tsx`).includes(`section="${section}"`));
  }
  assert.match(read("src/components/ProfileScreen.tsx"), /href="\/profile\/subscription"/);
  assert.match(read("src/components/ProfileScreen.tsx"), /href="\/profile\/english-level"/);
  assert.match(read("src/components/InterestsScreen.tsx"), /href="\/profile"/);
  assert.match(read("app/profile/interests/page.tsx"), /<InterestsScreen\s*\/>/);
});
