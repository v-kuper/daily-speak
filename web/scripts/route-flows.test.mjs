import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";
import { configureStore } from "@reduxjs/toolkit";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Provider } from "react-redux";
import { AppRouterContext } from "next/dist/shared/lib/app-router-context.shared-runtime.js";
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
  let pathname = "/speak";
  return {
    visits, currentPath: () => pathname,
    push: (path) => { pathname = path; visits.push(["push", path]); },
    replace: (path) => { pathname = path; visits.push(["replace", path]); },
  };
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
  const attempt = run(store, router, draft, upload.promise, router.currentPath);
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
    await run(store, router, draft, failure === "upload" ? Promise.reject(new Error("upload failed")) : null, router.currentPath);
    assert.equal(requests.length, failure === "upload" ? 1 : 2);
    assert.deepEqual(router.visits, [["push", "/history/local-123"], ["replace", "/history/permanent-123"]]);
    assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["permanent-123"]);
    assert.equal(store.getState().app.recordingSaveError, null);
  });
}

test("terminal save failure returns to history and retains a visible failed recording", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  server(t, async () => response({ error: "Storage unavailable" }, 503));
  await run(store, router, draft, null, router.currentPath);
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
  const attempt = run(store, router, draft, null, router.currentPath);
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

for (const status of [403, 404]) {
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

// These exercise the same lifecycle controllers that the screen effects mount.
const settle = () => new Promise((resolve) => setImmediate(resolve));
const schedulerFor = () => {
  const callbacks = new Set();
  return {
    setInterval: (callback) => { callbacks.add(callback); return callback; },
    clearInterval: (callback) => callbacks.delete(callback),
    tick: () => Promise.all([...callbacks].map((callback) => callback())),
    get size() { return callbacks.size; },
  };
};
const renderDetails = (store, recordingId) => renderToStaticMarkup(createElement(
  Provider, { store }, createElement(AppRouterContext.Provider, { value: routerFor() },
    createElement(load("src/components/DetailsScreen.tsx").default, { recordingId })),
));

for (const outcome of ["success", "fallback", "failure"]) {
  test(`background ${outcome} after leaving local details preserves the newer recording and location`, async (t) => {
    const store = storeFor({ isAuthenticated: true }), router = routerFor(), primary = deferred();
    server(t, async (url) => url.endsWith("/finish") ? primary.promise
      : outcome === "failure" ? response({ error: "Storage unavailable" }, 503) : response({ recording: saved }));
    const attempt = flow("saveAndNavigate")(store, router, draft, null, router.currentPath);
    router.push("/speak");
    store.dispatch(app.startFreeTalk());
    store.dispatch(app.tickRecording());
    primary.resolve(outcome === "success" ? response({ recording: saved }) : response({ error: "Session unavailable" }, 503));
    await attempt;
    assert.equal(router.currentPath(), "/speak");
    assert.deepEqual(router.visits, [["push", "/history/local-123"], ["push", "/speak"]]);
    assert.equal(store.getState().app.speakState, "recording");
    assert.equal(store.getState().app.recordingDuration, 1);
    assert.equal(store.getState().app.recordings[0].id, outcome === "failure" ? "local-123" : "permanent-123");
  });
}

test("post-auth save 401 invalidates the session, preserves guest audio, and allows re-authentication and one retry", async (t) => {
  const store = storeFor(guest), router = routerFor();
  let logins = 0, saves = 0;
  server(t, async (url, init) => {
    if (url.endsWith("/login")) {
      logins++;
      return response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } });
    }
    saves++;
    assert.equal(JSON.parse(init.body).recording.audioDataUrl, draft.audioDataUrl);
    return saves === 1 ? response({ error: "Unauthorized" }, 401) : response({ recording: saved });
  });
  await flow("authenticateAndNavigate")(store, router, "signIn", "/speak");
  const expired = store.getState().app;
  assert.equal(expired.isAuthenticated, false);
  assert.equal(expired.pendingRecordingAudioDataUrl, draft.audioDataUrl);
  assert.equal(expired.pendingSaveAfterAuth, true);
  assert.ok(expired.recordingSaveError);
  assert.ok(expired.authError);
  assert.deepEqual(router.visits, []);
  store.dispatch(app.setAuthPasswordDraft("password123"));
  await flow("authenticateAndNavigate")(store, router, "signIn", "/speak");
  assert.equal(logins, 2);
  assert.equal(saves, 2);
  assert.equal(store.getState().app.pendingSaveAfterAuth, false);
  assert.deepEqual(router.visits, [["replace", "/history/permanent-123"]]);
});

test("background save 401 preserves both its retry draft and a newer speaking draft without an unauthorized fallback", async (t) => {
  const store = storeFor({ isAuthenticated: true, userEmail: "person@example.test" }), router = routerFor(), primary = deferred();
  const secondAudio = "data:audio/webm;base64,ZGVm";
  const requests = [];
  server(t, async (url, init) => {
    requests.push(url);
    if (url.endsWith("/finish")) return primary.promise;
    if (url.endsWith("/login")) return response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } });
    assert.equal(JSON.parse(init.body).recording.audioDataUrl, draft.audioDataUrl);
    return response({ recording: saved });
  });
  const attempt = flow("saveAndNavigate")(store, router, draft, null, router.currentPath);
  router.push("/speak");
  store.dispatch(app.startFreeTalk());
  store.dispatch(app.tickRecording());
  store.dispatch(app.stopRecording());
  store.dispatch(app.setRecordingAudioDataUrl(secondAudio));
  primary.resolve(response({ error: "Unauthorized" }, 401));
  await attempt;
  const expired = store.getState().app;
  assert.equal(expired.isAuthenticated, false);
  assert.equal(requests.length, 1);
  assert.equal(expired.pendingRecordingAudioDataUrl, secondAudio);
  assert.equal(expired.pendingAuthSaveDraft.audioDataUrl, draft.audioDataUrl);
  assert.equal(expired.pendingAuthSaveDraft.recordingUploadSessionId, null);
  assert.equal(router.currentPath(), "/speak");
  store.dispatch(app.setAuthPasswordDraft("password123"));
  await flow("authenticateAndNavigate")(store, router, "signIn", "/speak");
  assert.deepEqual(requests.map((url) => new URL(url).pathname), [
    "/api/recording-sessions/upload-123/finish", "/api/auth/login", "/api/user/recordings",
  ]);
  assert.equal(store.getState().app.pendingAuthSaveDraft, null);
  assert.equal(store.getState().app.pendingRecordingAudioDataUrl, secondAudio);
  assert.equal(store.getState().app.speakState, "recorded");
});

test("a later user-data 401 preserves the already recovered background audio and visible error", async (t) => {
  const store = storeFor({ isAuthenticated: true, userEmail: "person@example.test" }), router = routerFor(), userData = deferred();
  server(t, async (url) => url.endsWith("/api/user/data") ? userData.promise : response({ error: "Unauthorized" }, 401));
  const fetching = store.dispatch(app.fetchUserData());
  await flow("saveAndNavigate")(store, router, draft, null, router.currentPath);
  const recovery = store.getState().app.pendingAuthSaveDraft;
  userData.resolve(response({ error: "Unauthorized" }, 401));
  await fetching;
  assert.equal(store.getState().app.pendingAuthSaveDraft?.audioDataUrl, draft.audioDataUrl);
  assert.deepEqual(store.getState().app.pendingAuthSaveDraft, recovery);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
  assert.ok(store.getState().app.recordingSaveError);
});

test("late resource 401 responses never erase a guest save awaiting re-authentication", () => {
  for (const name of ["fetchUserData", "saveInterests", "retryRecordingProcessing", "generateShadowingAudio", "deleteRecording", "subscribeMonthly", "cancelSubscription", "saveEnglishLevel"]) {
    const before = { ...initial(), ...guest, isAuthenticated: false, recordingSaveError: "Session expired" };
    const after = app.default(before, app[name].rejected(null, "late-request", "recording-1", "Unauthorized"));
    assert.equal(after.pendingRecordingAudioDataUrl, draft.audioDataUrl, name);
    assert.equal(after.pendingSaveAfterAuth, true, name);
    assert.equal(after.recordingSaveError, "Session expired", name);
  }
});

for (const outcome of ["success", "failure"]) {
  test(`a ${outcome} completed before Next mounts local details reconciles only on that local route`, async (t) => {
    const store = storeFor({ isAuthenticated: true }), router = routerFor();
    let pathname = "/speak";
    // Next push returns before the browser commits the dynamic route.
    router.push = (path) => router.visits.push(["push", path]);
    const replace = router.replace;
    router.replace = (path) => { pathname = path; replace(path); };
    server(t, async () => outcome === "success" ? response({ recording: saved }) : response({ error: "Unavailable" }, 503));
    await flow("saveAndNavigate")(store, router, draft, null, () => pathname);
    assert.equal(pathname, "/speak");
    const reconcile = flow("reconcileRecordingSaveRoute");
    reconcile(store, router, "local-123", () => pathname);
    assert.equal(router.visits.length, 1, "a different active route must remain untouched");
    pathname = "/history/local-123";
    reconcile(store, router, "local-123", () => pathname);
    assert.equal(pathname, outcome === "success" ? "/history/permanent-123" : "/history");
  });
}

test("detail session expiry preserves an unsent speaking draft through re-authentication", async (t) => {
  const store = storeFor({ ...guest, isAuthenticated: true, pendingSaveAfterAuth: false }), router = routerFor();
  server(t, async (url) => url.endsWith("/login")
    ? response({ user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" } })
    : response({ error: "Unauthorized" }, 401));
  await store.dispatch(app.fetchRecording("other-recording"));
  assert.equal(store.getState().app.pendingRecordingAudioDataUrl, draft.audioDataUrl);
  store.dispatch(app.setAuthPasswordDraft("password123"));
  await flow("authenticateAndNavigate")(store, router, "signIn", "/history");
  assert.equal(store.getState().app.pendingRecordingAudioDataUrl, draft.audioDataUrl);
  assert.equal(store.getState().app.speakState, "recorded");
});

test("history lifecycle stops requests after 401 and its guard sends the user to re-authentication", async (t) => {
  const start = flow("startHistoryRecordingPolling"), store = storeFor({ isAuthenticated: true, authInitialized: true, recordings: [saved] });
  const scheduler = schedulerFor();
  let requests = 0;
  server(t, async () => { requests++; return response({ error: "Unauthorized" }, 401); });
  const stop = start(store, scheduler);
  await settle();
  assert.equal(store.getState().app.isAuthenticated, false);
  const routes = load("src/lib/routes.ts");
  assert.equal(routes.protectedRouteDestination(true, store.getState().app.isAuthenticated, "/history"), "/auth?returnTo=%2Fhistory");
  await scheduler.tick();
  await scheduler.tick();
  assert.equal(requests, 1);
  stop();
  assert.equal(scheduler.size, 0);
});

for (const failure of ["network", "503"]) {
  test(`detail ${failure} failure retains cached content, pauses polling, and recovers through explicit retry`, async (t) => {
    const select = flow("recordingDetailState"), store = storeFor({ isAuthenticated: true, recordings: [saved] });
    let recover = false, requests = 0;
    server(t, async () => {
      requests++;
      if (!recover) {
        if (failure === "network") throw new Error("offline");
        return response({ error: "Temporarily unavailable" }, 503);
      }
      return response({ recording: saved });
    });
    // Reproduce the lost cached content against the existing production selector first.
    await store.dispatch(app.fetchRecording(saved.id));
    assert.equal(select(store.getState().app, saved.id).recording?.id, saved.id);
    assert.equal(select(store.getState().app, saved.id).canRetry, true);
    assert.match(renderDetails(store, saved.id), /Retry loading recording/);
    assert.match(renderDetails(store, saved.id), /Travel/);
    const scheduler = schedulerFor();
    const stop = flow("startRecordingDetailLifecycle")(store, saved.id, scheduler);
    await settle();
    await scheduler.tick();
    assert.equal(requests, 1);
    recover = true;
    await flow("retryRecordingFetch")(store, saved.id);
    assert.equal(select(store.getState().app, saved.id).error, null);
    await scheduler.tick();
    assert.equal(requests, 3, "successful manual retry resumes processing polling");
    stop();
    assert.equal(scheduler.size, 0);
  });
}

test("unloaded detail lifecycle exposes a recoverable error without request loops and loads after retry", async (t) => {
  const start = flow("startRecordingDetailLifecycle"), store = storeFor({ isAuthenticated: true }), scheduler = schedulerFor();
  let recover = false, requests = 0;
  server(t, async () => {
    requests++;
    return recover ? response({ recording: { ...saved, status: "ready", shadowingStatus: "ready" } })
      : response({ error: "Temporarily unavailable" }, 503);
  });
  const stop = start(store, saved.id, scheduler);
  await settle();
  const failed = flow("recordingDetailState")(store.getState().app, saved.id);
  assert.equal(failed.recording, undefined);
  assert.equal(failed.canRetry, true);
  assert.ok(failed.error);
  assert.match(renderDetails(store, saved.id), /Retry loading recording/);
  await scheduler.tick();
  assert.equal(requests, 1);
  recover = true;
  await flow("retryRecordingFetch")(store, saved.id);
  assert.equal(flow("recordingDetailState")(store.getState().app, saved.id).recording.id, saved.id);
  await scheduler.tick();
  assert.equal(requests, 2);
  stop();
});

test("terminal detail errors hide cached content and cannot be retried by the lifecycle", async (t) => {
  const store = storeFor({ isAuthenticated: true, recordings: [saved] }), scheduler = schedulerFor();
  let requests = 0;
  server(t, async () => { requests++; return response({ error: "Recording not found" }, 404); });
  const stop = flow("startRecordingDetailLifecycle")(store, saved.id, scheduler);
  await settle();
  const failed = flow("recordingDetailState")(store.getState().app, saved.id);
  assert.equal(failed.recording, undefined);
  assert.equal(failed.canRetry, false);
  await scheduler.tick();
  await flow("retryRecordingFetch")(store, saved.id);
  assert.equal(requests, 1);
  stop();
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
