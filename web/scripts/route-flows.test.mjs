import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";
import { configureStore } from "@reduxjs/toolkit";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Provider } from "react-redux";
import { AppRouterContext } from "next/dist/shared/lib/app-router-context.shared-runtime.js";
import { SearchParamsContext } from "next/dist/shared/lib/hooks-client-context.shared-runtime.js";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const app = load("src/store/slices/appSlice.ts");
const api = load("src/lib/apiClient.ts");
const identityClient = load("src/lib/identity.ts");
const recordingDraftAudio = load("src/lib/recordingDraftAudio.ts");
// Keep missing behavior an assertion failure during RED, rather than a module-load error.
const flows = existsSync("src/lib/routeFlows.ts") ? load("src/lib/routeFlows.ts") : {};
const initial = () => app.default(undefined, { type: "test/init" });
const storeFor = (overrides = {}) => configureStore({
  reducer: { app: app.default }, preloadedState: { app: { ...initial(), ...overrides } },
});
let draft = {
  localRecordingId: "local-123", topic: "Travel",
  duration: 20, timestamp: "2026-09-21T10:00:00Z", practiceType: "topic",
  audioStorageKey: "recording-audio:test-placeholder", photoDataUrl: null, photoObject: null,
};
const saved = {
  id: "permanent-123", topic: "Travel", duration: 20, timestamp: draft.timestamp,
  practiceType: "topic", status: "processing", transcript: "", correctedTranscript: "",
  suggestions: [], processingStage: "transcription", photoObject: null, processingError: null,
  shadowingStatus: "pending", shadowingError: null, shadowingUpdatedAt: draft.timestamp,
  media: {
    audio: { assetId: "audio-asset", downloadPath: "/api/v1/media/audio-asset/download" },
    photo: null,
    shadowing: null,
  },
};
let guest = {
  authInitialized: true, authEmailDraft: "person@example.test", authPasswordDraft: "password123",
  speakState: "recorded", selectedTopic: "Travel", recordingDuration: 20,
  pendingRecordingAudioStorageKey: draft.audioStorageKey, pendingSaveAfterAuth: true,
};
test.beforeEach(async () => {
  const audioStorageKey = await recordingDraftAudio.storeRecordingDraftAudio(
    new Blob(["abc"], { type: "audio/webm" }),
  );
  draft = { ...draft, audioStorageKey };
  guest = { ...guest, pendingRecordingAudioStorageKey: audioStorageKey };
});
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
const readyMedia = (assetId = "audio-asset") => response({
  asset: { id: assetId, state: "ready" },
  upload: { id: `upload-${assetId}`, state: "completed", partSizeBytes: 8388608, partCount: 1 },
});
const identityResponse = (promotion) => ({
  principal: { id: "user-principal", type: "user" },
  user: { email: "person@example.test", isSubscriber: false, englishLevel: "B1" },
  session: { id: "web-session", platform: "web" },
  tokens: {
    tokenType: "Bearer", accessToken: "user-access",
    accessTokenExpiresAt: "2099-01-01T00:15:00Z",
  },
  ...(promotion ? { guestPreviewPromotion: promotion } : {}),
});
function server(t, handler) {
  // Only the network is replaced: real API encoding, thunks, reducers, and route flow run.
  identityClient.forgetBrowserIdentity();
  t.after(() => identityClient.forgetBrowserIdentity());
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
      if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
      assert.equal(url, "https://api.example.test/api/v1/recordings");
      assert.equal(JSON.parse(init.body).audioAssetId, "audio-asset");
      return response({ recording: saved });
    });
    const attempt = run(store, router, mode, "/profile");
    await run(store, router, mode, "/profile");
    assert.deepEqual(router.visits, []);
    pending.resolve(response(identityResponse()));
    await attempt;
    assert.equal(requests.length, 3);
    assert.deepEqual(router.visits, [["replace", "/history/permanent-123"]]);
    assert.equal(store.getState().app.pendingSaveAfterAuth, false);
    assert.equal(store.getState().app.pendingRecordingAudioStorageKey, null);
  });
}

test("auth returnTo is validated and rejected sign-in stays with a visible Redux error", async (t) => {
  const run = flow("authenticateAndNavigate");
  server(t, async () => response(identityResponse()));
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
  server(t, async (url) => {
    if (url.endsWith("/login")) return response(identityResponse());
    if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
    saveCalls++;
    return response({ error: { code: "storage_unavailable", message: "Storage unavailable" } }, 503);
  });
  await run(store, router, "signIn", "/profile");
  assert.deepEqual(router.visits, []);
  assert.equal(saveCalls, 1);
  assert.equal(store.getState().app.recordingSaveError, "Storage unavailable");
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, draft.audioStorageKey);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
});

test("guest save retains the recording and cancel clears both auth drafts and returns to speaking", () => {
  const store = storeFor({ ...guest, pendingSaveAfterAuth: false }), router = routerFor();
  flow("startGuestSave")(store, router);
  assert.deepEqual(router.visits, [["push", "/auth?returnTo=%2Fspeak"]]);
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, draft.audioStorageKey);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
  flow("cancelAuthentication")(store, router);
  assert.equal(store.getState().app.authEmailDraft, "");
  assert.equal(store.getState().app.authPasswordDraft, "");
  assert.equal(store.getState().app.pendingSaveAfterAuth, false);
  assert.deepEqual(router.visits.at(-1), ["replace", "/speak"]);
});

test("authenticated save immediately opens the local recording and replaces it after v1 media-backed creation", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  const save = deferred(), recordingStarted = deferred();
  let saves = 0;
  server(t, async (url) => {
    if (url.endsWith("/api/v1/media/uploads")) {
      return response({ asset: { id: "audio-asset", state: "ready" }, upload: { id: "upload-123", state: "completed", partSizeBytes: 8388608, partCount: 1 } });
    }
    assert.equal(url, "https://api.example.test/api/v1/recordings");
    saves++;
    recordingStarted.resolve("started");
    return save.promise;
  });
  const attempt = run(store, router, draft, router.currentPath);
  assert.deepEqual(router.visits, [["push", "/history/local-123"]]);
  assert.equal(store.getState().app.recordings[0].id, "local-123");
  const firstOutcome = await Promise.race([
    recordingStarted.promise,
    attempt.then(() => "completed"),
  ]);
  assert.equal(firstOutcome, "started", "save flow completed before creating the recording");
  assert.equal(saves, 1);
  assert.equal(router.visits.length, 1);
  save.resolve(response({ recording: saved }));
  await attempt;
  assert.deepEqual(router.visits.at(-1), ["replace", "/history/permanent-123"]);
  assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["permanent-123"]);
});

test("terminal save failure keeps a retryable local recording until upload succeeds", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({
    isAuthenticated: true,
    isSubscriber: false,
    weeklyRemainingSeconds: 0,
  }), router = routerFor();
  let unavailable = true;
  server(t, async (url) => {
    if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
    return unavailable
      ? response({ error: { code: "storage_unavailable", message: "Storage unavailable" } }, 503)
      : response({ recording: saved });
  });
  await run(store, router, draft, router.currentPath);
  assert.deepEqual(router.visits, [["push", "/history/local-123"]]);
  assert.equal(store.getState().app.recordingSaveError, "Storage unavailable");
  assert.equal(store.getState().app.recordings[0].status, "failed");
  assert.equal(store.getState().app.backgroundSaveRecordingId, null);
  assert.equal(store.getState().app.recordingSaveDrafts["local-123"].audioStorageKey, draft.audioStorageKey);

  unavailable = false;
  await run(store, router, store.getState().app.recordingSaveDrafts["local-123"], router.currentPath);
  assert.deepEqual(router.visits.at(-1), ["replace", "/history/permanent-123"]);
  assert.equal(store.getState().app.recordingSaveDrafts["local-123"], undefined);
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

test("the local processing placeholder keeps the completed interview conversation", () => {
  const store = storeFor({ isAuthenticated: true });
  const interviewTurns = [{
    sequence: 1,
    question: "Where did you travel?",
    askedAtMs: 0,
    endedAtMs: 4000,
    answerText: "I went to Rome.",
    answerSource: "final",
    answerAlignment: null,
  }];
  store.dispatch(app.showBackgroundRecordingSave({ ...draft, interviewTurns }));

  assert.deepEqual(store.getState().app.recordings[0].interviewTurns, interviewTurns);
  assert.deepEqual(store.getState().app.recordingSaveDrafts["local-123"].interviewTurns, interviewTurns);
});

test("session expiry preserves a failed upload draft for re-authentication", () => {
  const store = storeFor({ isAuthenticated: true });
  store.dispatch(app.showBackgroundRecordingSave(draft));
  store.dispatch(app.saveRecording.rejected(null, "save", draft, "Storage unavailable"));
  store.dispatch(app.saveInterests.rejected(null, "expire", undefined, "Unauthorized"));
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
  assert.equal(store.getState().app.pendingAuthSaveDraft?.localRecordingId, "local-123");
  assert.equal(store.getState().app.pendingAuthSaveDraft?.audioStorageKey, draft.audioStorageKey);
});

test("user bootstrap loads recordings only from the v1 collection", async (t) => {
  const store = storeFor({ isAuthenticated: true, userEmail: "person@example.test" });
  const requests = [];
  server(t, async (url) => {
    requests.push(url);
    if (url.endsWith("/api/v1/profile")) {
      return response({
        interestIds: [], englishLevel: "B1", quota: null, subscription: null,
        recordings: [{ ...saved, id: "legacy-bootstrap-recording" }],
      });
    }
    assert.equal(url, "https://api.example.test/api/v1/recordings?limit=100");
    return response({ items: [saved], page: { limit: 100, nextCursor: null } });
  });

  const result = await store.dispatch(app.fetchUserData()).unwrap();
  assert.deepEqual(requests, [
    "https://api.example.test/api/v1/profile",
    "https://api.example.test/api/v1/recordings?limit=100",
  ]);
  assert.deepEqual(result.recordings.map(({ id }) => id), ["permanent-123"]);
  assert.deepEqual(store.getState().app.recordings.map(({ id }) => id), ["permanent-123"]);
});

test("fallback remains a background save while the fallback response is pending", async (t) => {
  const run = flow("saveAndNavigate"), store = storeFor({ isAuthenticated: true }), router = routerFor();
  const fallback = deferred();
  server(t, async (url) => url.endsWith("/api/v1/media/uploads") ? readyMedia() : fallback.promise);
  const attempt = run(store, router, draft, router.currentPath);
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
      assert.equal(url, "https://api.example.test/api/v1/recordings/other%20owner");
      assert.equal(init.credentials, "include");
      return response({
        error: { code: "not_found", message: "Recording not accessible", requestId: "request-detail" },
      }, status);
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
  server(t, async (url, init) => {
    assert.equal(url, "https://api.example.test/api/v1/recordings/permanent-123");
    assert.equal(init.method, "DELETE");
    return pending.promise;
  });
  const attempt = run(store, router, "permanent-123");
  assert.deepEqual(router.visits, []);
  pending.resolve(response({ deletedRecordingId: "permanent-123" }));
  await attempt;
  assert.deepEqual(router.visits, [["replace", "/history"]]);
  assert.deepEqual(store.getState().app.recordings, []);
  server(t, async () => response({
    error: { code: "internal_error", message: "Cannot delete", requestId: "request-delete" },
  }, 503));
  await run(store, router, "another-id");
  assert.equal(router.visits.length, 1);
  assert.equal(store.getState().app.recordingDeleteError, "Cannot delete");
});

test("deleting a failed local upload removes its retry draft and staged audio", async () => {
  const store = storeFor({ isAuthenticated: true }), router = routerFor();
  store.dispatch(app.showBackgroundRecordingSave(draft));
  store.dispatch(app.saveRecording.rejected(null, "save", draft, "Storage unavailable"));

  await flow("deleteAndNavigate")(store, router, draft.localRecordingId);

  assert.deepEqual(router.visits, [["replace", "/history"]]);
  assert.equal(store.getState().app.recordings.some(({ id }) => id === draft.localRecordingId), false);
  assert.equal(store.getState().app.recordingSaveDrafts[draft.localRecordingId], undefined);
  await assert.rejects(
    () => recordingDraftAudio.loadRecordingDraftAudio(draft.audioStorageKey),
    /no longer available/i,
  );
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

for (const practiceType of ["topic", "free_talk", "photo_description"]) {
  test(`discarding an unsaved ${practiceType} returns to the start and removes its audio, photo, and auth-save draft`, async (t) => {
    const store = storeFor({ ...guest, isAuthenticated: true, recordingPracticeType: practiceType,
      pendingPhotoDataUrl: "data:image/png;base64,YQ==", pendingPhotoObjectDraft: "Old photo",
      pendingAuthSaveDraft: draft, recordingInputError: "Old error", recordingSaveError: "Old save error",
      topicGuidanceWords: ["old"], topicGuidanceTopic: "Old topic", recordings: [saved],
    });
    const router = routerFor();
    server(t, () => { assert.fail("discard must not submit a new recording"); });
    const work = flow("discardRecordingAndNavigate")(store, router);
    assert.equal(store.getState().app.speakState, "idle");
    await work;
    const state = store.getState().app;
    for (const field of ["selectedTopic", "pendingRecordingAudioStorageKey", "pendingPhotoDataUrl", "pendingAuthSaveDraft", "recordingInputError", "recordingSaveError"]) assert.equal(state[field], null, field);
    assert.equal(state.recordingDuration, 0);
    assert.equal(state.pendingPhotoObjectDraft, "");
    assert.equal(state.pendingSaveAfterAuth, false);
    assert.deepEqual(state.topicGuidanceWords, []);
    assert.deepEqual(state.recordings, [saved]);
    assert.deepEqual(router.visits, [["replace", "/speak"]]);
    await assert.rejects(recordingDraftAudio.loadRecordingDraftAudio(draft.audioStorageKey), /no longer available/i);
  });
}

test("discard cannot interrupt a recording already being saved", async () => {
  const store = storeFor({ ...guest, recordingSaveStatus: "loading" }), router = routerFor();
  await flow("discardRecordingAndNavigate")(store, router);
  assert.equal(store.getState().app.speakState, "recorded");
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, draft.audioStorageKey);
  assert.ok(await recordingDraftAudio.loadRecordingDraftAudio(draft.audioStorageKey));
  assert.deepEqual(router.visits, []);
});

test("completed recording offers a discard action without repeating its account limit", () => {
  const markup = renderToStaticMarkup(createElement(Provider, { store: storeFor({ ...guest, isAuthenticated: true }) },
    createElement(AppRouterContext.Provider, { value: routerFor() }, createElement(load("src/components/SpeakScreen.tsx").default))));
  assert.match(markup, /Recording complete/);
  assert.match(markup, /Выйти без сохранения/);
  assert.doesNotMatch(markup, /Account recording limit/);
});

test("history calendar filters recordings by date, describes days with recordings, and links to their detail", () => {
  const first = { ...saved, id: "first", timestamp: "2026-09-30T12:00:00Z" };
  const older = { ...saved, id: "older", timestamp: "2026-08-10T12:00:00Z" };
  const store = storeFor({ recordings: [first, older], calendarVisible: true });
  store.dispatch(app.setCalendarDate("2026-09-30"));
  assert.equal(store.getState().app.calendarMonth, 8);
  const markup = renderToStaticMarkup(createElement(Provider, { store },
    createElement(AppRouterContext.Provider, { value: routerFor() },
      createElement(SearchParamsContext.Provider, { value: new URLSearchParams("date=2026-09-30") },
        createElement(load("src/components/HistoryScreen.tsx").default)))));
  assert.match(markup, /href="\/history\/first"/);
  assert.doesNotMatch(markup, /href="\/history\/older"/);
  assert.match(markup, /Все записи/);
  assert.match(markup, /aria-label="30 сентября 2026 г., 1 запись" aria-pressed="true"/);
  assert.match(markup, />Пн<[^]*>Вт<[^]*>Ср<[^]*>Чт<[^]*>Пт<[^]*>Сб<[^]*>Вс</);
});

test("the live interview shows only the current question without an accumulated transcript", () => {
  const InterviewQuestionCard = load("src/components/InterviewQuestionCard.tsx").default;
  const markup = renderToStaticMarkup(createElement(InterviewQuestionCard, {
    turns: [{
      seq: 1,
      question: "Where did you travel?",
      askedAtMs: 65000,
      endedAtMs: 69000,
      provisionalTranscript: "I went to Rome.",
      transcriptStatus: "ready",
    }, {
      seq: 2,
      question: "What did you enjoy there?",
      askedAtMs: 70000,
      endedAtMs: null,
      provisionalTranscript: "",
      transcriptStatus: "pending",
    }],
    canAdvance: true,
    onNext: () => {},
    liveTranscriptionAvailable: true,
    liveCaption: null,
    hasAnswerEvidence: false,
    boundaryPending: false,
  }));

  assert.match(markup, /Question 2/);
  assert.match(markup, /What did you enjoy there\?/);
  assert.doesNotMatch(markup, /Where did you travel\?|I went to Rome\./);
  assert.doesNotMatch(markup, /Conversation transcript|conversation-transcript|Interviewer:|You:|01:05|01:10/);
});

test("the live interview renders only the latest ephemeral subtitle phrase", () => {
  const InterviewQuestionCard = load("src/components/InterviewQuestionCard.tsx").default;
  const markup = renderToStaticMarkup(createElement(InterviewQuestionCard, {
    turns: [{
      seq: 1,
      question: "Tell me about your day.",
      askedAtMs: 0,
      endedAtMs: null,
      provisionalTranscript: "",
      transcriptStatus: "pending",
      liveTranscriptFinal: "I went ",
      liveTranscriptInterim: "to work",
    }],
    canAdvance: true,
    onNext: () => {},
    liveTranscriptionAvailable: true,
    liveCaption: "to work",
    hasAnswerEvidence: true,
    boundaryPending: false,
  }));

  assert.equal((markup.match(/interview-live-caption/g) ?? []).length, 1);
  assert.match(markup, /class="interview-live-caption"[^>]*>to work<\/div>/);
  assert.doesNotMatch(markup, /I went|Conversation transcript|conversation-transcript|Live subtitles show what was recognized/);
});

test("the local topic processing route immediately renders its saved conversation snapshot", () => {
  const recording = {
    ...saved,
    id: "local-topic",
    status: "processing",
    processingStage: null,
    localAudioStorageKey: draft.audioStorageKey,
    media: null,
    interviewTurns: [{
      sequence: 1,
      question: "Where did you travel?",
      askedAtMs: 0,
      endedAtMs: 4000,
      answerText: "I went to Rome.",
      answerSource: "final",
      answerAlignment: null,
    }],
  };
  const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);

  assert.match(markup, /Conversation transcript/);
  assert.match(markup, /Where did you travel\?/);
  assert.match(markup, /I went to Rome\./);
});

test("topic shadowing restores corrected learner answers and hides experimental samples", () => {
  const recording = {
    ...saved,
    status: "ready",
    processingStage: null,
    transcript: "I goed home yesterday.",
    correctedTranscript: "I went home yesterday.",
    shadowingScript: {
      englishLevel: "a2", text: "Did you say I goed home? I went to the park yesterday.",
      turns: [{ sequence: 1, question: "Did you say I goed home?", answerText: "I went to the park yesterday." }],
    },
    suggestions: [{
      wrong: "I goed home",
      right: "I went home",
      explanation: "Use the past tense went.",
      severity: "major",
    }],
    interviewTurns: [{
      sequence: 1,
      question: "Did you say I goed home?",
      askedAtMs: 0,
      endedAtMs: 3000,
      answerText: "I goed home yesterday.",
      correctedAnswerText: "I went home yesterday.",
      answerSource: "final",
      answerAlignment: null,
    }],
  };
  const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);

  assert.match(markup, /Conversation transcript/);
  assert.equal((markup.match(/conversation-transcript/g) ?? []).length, 2);
  assert.equal((markup.match(/Interviewer:<\/strong> Did you say I goed home\?/g) ?? []).length, 2);
  assert.match(markup, /You:<\/strong>/);
  assert.match(markup, /Shadowing practice[\s\S]*Ваш ответ в естественной форме[\s\S]*I went home yesterday\./);
  assert.doesNotMatch(markup, /I went to the park yesterday\./);
  assert.equal((markup.match(/<mark/g) ?? []).length, 1);
  assert.doesNotMatch(markup, /Interview timeline|interview-timeline/);
});

test("legacy topic results retain their corrected transcript for shadowing", () => {
  const recording = {
    ...saved,
    status: "ready",
    processingStage: null,
    correctedTranscript: "I went home yesterday.",
    interviewTurns: [{
      sequence: 1,
      question: "Where did you go?",
      askedAtMs: 0,
      endedAtMs: 3000,
      answerText: "I goed home yesterday.",
      answerSource: "final",
      answerAlignment: null,
    }],
  };
  const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);

  assert.equal((markup.match(/Interviewer:<\/strong>/g) ?? []).length, 1);
  assert.match(markup, /Shadowing practice[\s\S]*I went home yesterday\./);
  assert.doesNotMatch(markup, /Natural answer is unavailable/);
});

test("answer practice follows shadowing and lists answered questions in their original order", () => {
  const recording = {
    ...saved, status: "ready", processingStage: null,
    transcript: "I goed home. I make dinner.", correctedTranscript: "I went home. I made dinner.",
    interviewTurns: [
      { sequence: 3, question: "What happened next?", answerText: "I make dinner.", answerSource: "final" },
      { sequence: 2, question: "Skipped question?", answerText: " ", answerSource: "none" },
      { sequence: 1, question: "Where did you go?", answerText: "I goed home.", answerSource: "final" },
    ],
  };
  const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);
  assert.ok(markup.indexOf("details-shadowing-material") < markup.indexOf("details-retake-material"));
  const practice = markup.slice(markup.indexOf("details-retake-material"));
  assert.match(practice, /Исправить свои ответы/);
  assert.match(practice, /Открыть практику по вопросу 1: Where did you go\?/);
  assert.match(practice, /Открыть практику по вопросу 2: What happened next\?/);
  assert.ok(practice.indexOf("Where did you go?") < practice.indexOf("What happened next?"));
  assert.doesNotMatch(practice, /Skipped question/);
});

test("partial corrected turns fall back to the full corrected learner transcript", () => {
  const recording = {
    ...saved,
    status: "ready",
    processingStage: null,
    correctedTranscript: "Where did you go? I went home. What happened next? I made dinner.",
    interviewTurns: [{
      sequence: 1,
      question: "Where did you go?",
      askedAtMs: 0,
      endedAtMs: 3000,
      answerText: "I goed home.",
      correctedAnswerText: "I went home.",
      answerSource: "final",
      answerAlignment: null,
    }, {
      sequence: 2,
      question: "What happened next?",
      askedAtMs: 3000,
      endedAtMs: 6000,
      answerText: "I make dinner.",
      answerSource: "final",
      answerAlignment: null,
    }],
  };
  const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);

  assert.equal((markup.match(/conversation-transcript/g) ?? []).length, 1);
  assert.match(markup, /Shadowing practice[\s\S]*Where did you go\? I went home\. What happened next\? I made dinner\./);
  assert.doesNotMatch(markup, /Natural answer is unavailable/);
});

for (const practiceType of ["free_talk", "photo_description"]) {
  test(`${practiceType} results keep the plain transcript`, () => {
    const recording = {
      ...saved,
      practiceType,
      status: "ready",
      processingStage: null,
      transcript: "Plain speaking transcript.",
      correctedTranscript: "Natural speaking transcript.",
      interviewTurns: [{
        sequence: 1,
        question: "A question that should not be rendered",
        askedAtMs: 0,
        endedAtMs: 3000,
        answerText: "An answer that should not be rendered",
        answerSource: "final",
        answerAlignment: null,
      }],
    };
    const markup = renderDetails(storeFor({ isAuthenticated: true, recordings: [recording] }), recording.id);

    assert.match(markup, /Plain speaking transcript\./);
    assert.match(markup, /Natural speaking transcript\./);
    assert.doesNotMatch(markup, /Conversation transcript|Interviewer:|You:/);
  });
}

for (const outcome of ["success", "failure"]) {
  test(`background ${outcome} after leaving local details preserves the newer recording and location`, async (t) => {
    const store = storeFor({ isAuthenticated: true }), router = routerFor(), primary = deferred();
    server(t, async (url) => url.endsWith("/api/v1/media/uploads") ? readyMedia() : primary.promise);
    const attempt = flow("saveAndNavigate")(store, router, draft, router.currentPath);
    router.push("/speak");
    store.dispatch(app.startFreeTalk());
    store.dispatch(app.tickRecording());
    primary.resolve(outcome === "success"
      ? response({ recording: saved })
      : response({ error: { code: "storage_unavailable", message: "Storage unavailable" } }, 503));
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
      return response(identityResponse());
    }
    if (url.endsWith("/api/v1/auth/refresh")) {
      return response({ error: { code: "invalid_refresh_token", message: "Refresh token is invalid" } }, 401);
    }
    if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
    saves++;
    assert.equal(JSON.parse(init.body).audioAssetId, "audio-asset");
    return saves === 1
      ? response({ error: { code: "unauthorized", message: "Unauthorized" } }, 401)
      : response({ recording: saved });
  });
  await flow("authenticateAndNavigate")(store, router, "signIn", "/speak");
  const expired = store.getState().app;
  assert.equal(expired.isAuthenticated, false);
  assert.equal(expired.pendingRecordingAudioStorageKey, draft.audioStorageKey);
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
  const secondAudio = await recordingDraftAudio.storeRecordingDraftAudio(
    new Blob(["def"], { type: "audio/webm" }),
  );
  const requests = [];
  server(t, async (url, init) => {
    requests.push(url);
    if (url.endsWith("/login")) return response(identityResponse());
    if (url.endsWith("/api/v1/auth/refresh")) {
      return response({ error: { code: "invalid_refresh_token", message: "Refresh token is invalid" } }, 401);
    }
    if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
    if (requests.filter((item) => item.endsWith("/api/v1/recordings")).length === 1) return primary.promise;
    assert.equal(JSON.parse(init.body).audioAssetId, "audio-asset");
    return response({ recording: saved });
  });
  const attempt = flow("saveAndNavigate")(store, router, draft, router.currentPath);
  router.push("/speak");
  store.dispatch(app.startFreeTalk());
  store.dispatch(app.tickRecording());
  store.dispatch(app.stopRecording());
  store.dispatch(app.setRecordingAudioStorageKey(secondAudio));
  primary.resolve(response({ error: { code: "unauthorized", message: "Unauthorized" } }, 401));
  await attempt;
  const expired = store.getState().app;
  assert.equal(expired.isAuthenticated, false);
  assert.ok(requests.some((url) => url.endsWith("/api/v1/recordings")));
  assert.equal(expired.pendingRecordingAudioStorageKey, secondAudio);
  assert.equal(expired.pendingAuthSaveDraft.audioStorageKey, draft.audioStorageKey);
  assert.equal(router.currentPath(), "/speak");
  store.dispatch(app.setAuthPasswordDraft("password123"));
  await flow("authenticateAndNavigate")(store, router, "signIn", "/speak");
  assert.ok(requests.some((url) => url.endsWith("/api/v1/auth/login")));
  assert.equal(requests.filter((url) => url.endsWith("/api/v1/recordings")).length, 2);
  assert.equal(store.getState().app.pendingAuthSaveDraft, null);
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, secondAudio);
  assert.equal(store.getState().app.speakState, "recorded");
});

test("a later user-data 401 preserves the already recovered background audio and visible error", async (t) => {
  const store = storeFor({ isAuthenticated: true, userEmail: "person@example.test" }), router = routerFor(), userData = deferred();
  server(t, async (url) => url.endsWith("/api/v1/profile") ? userData.promise : response({ error: { code: "unauthorized", message: "Unauthorized", requestId: "test" } }, 401));
  const fetching = store.dispatch(app.fetchUserData());
  await flow("saveAndNavigate")(store, router, draft, router.currentPath);
  const recovery = store.getState().app.pendingAuthSaveDraft;
  userData.resolve(response({ error: "Unauthorized" }, 401));
  await fetching;
  assert.equal(store.getState().app.pendingAuthSaveDraft?.audioStorageKey, draft.audioStorageKey);
  assert.deepEqual(store.getState().app.pendingAuthSaveDraft, recovery);
  assert.equal(store.getState().app.pendingSaveAfterAuth, true);
  assert.ok(store.getState().app.recordingSaveError);
});

test("late resource 401 responses never erase a guest save awaiting re-authentication", () => {
  for (const name of ["fetchUserData", "saveInterests", "retryRecordingProcessing", "generateShadowingAudio", "deleteRecording", "subscribeMonthly", "cancelSubscription", "saveEnglishLevel"]) {
    const before = { ...initial(), ...guest, isAuthenticated: false, recordingSaveError: "Session expired" };
    const after = app.default(before, app[name].rejected(null, "late-request", "recording-1", "Unauthorized"));
    assert.equal(after.pendingRecordingAudioStorageKey, draft.audioStorageKey, name);
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
    server(t, async (url) => {
      if (url.endsWith("/api/v1/media/uploads")) return readyMedia();
      return outcome === "success"
        ? response({ recording: saved })
        : response({ error: { code: "service_unavailable", message: "Unavailable" } }, 503);
    });
    await flow("saveAndNavigate")(store, router, draft, () => pathname);
    assert.equal(pathname, "/speak");
    const reconcile = flow("reconcileRecordingSaveRoute");
    reconcile(store, router, "local-123", () => pathname);
    assert.equal(router.visits.length, 1, "a different active route must remain untouched");
    pathname = "/history/local-123";
    reconcile(store, router, "local-123", () => pathname);
    assert.equal(pathname, outcome === "success" ? "/history/permanent-123" : "/history/local-123");
    if (outcome === "failure") {
      assert.equal(store.getState().app.recordingSaveDrafts["local-123"].audioStorageKey, draft.audioStorageKey);
    }
  });
}

test("detail session expiry preserves an unsent speaking draft through re-authentication", async (t) => {
  const store = storeFor({ ...guest, isAuthenticated: true, pendingSaveAfterAuth: false }), router = routerFor();
  server(t, async (url) => url.endsWith("/login")
    ? response(identityResponse())
    : response({ error: "Unauthorized" }, 401));
  await store.dispatch(app.fetchRecording("other-recording"));
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, draft.audioStorageKey);
  store.dispatch(app.setAuthPasswordDraft("password123"));
  await flow("authenticateAndNavigate")(store, router, "signIn", "/history");
  assert.equal(store.getState().app.pendingRecordingAudioStorageKey, draft.audioStorageKey);
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

test("a cached ready list item is hydrated once so interview turns appear after a reload", async (t) => {
  const listRecord = {
    ...saved,
    status: "ready",
    processingStage: null,
    transcript: "I goed home.",
    correctedTranscript: "Where did you go? I went home.",
    shadowingStatus: "ready",
  };
  const detailRecord = {
    ...listRecord,
    interviewTurns: [{
      sequence: 1,
      question: "Where did you go?",
      askedAtMs: 0,
      endedAtMs: 3000,
      answerText: "I goed home.",
      correctedAnswerText: "I went home.",
      answerSource: "final",
      answerAlignment: null,
    }],
  };
  const store = storeFor({ isAuthenticated: true, recordings: [listRecord] });
  const scheduler = schedulerFor();
  let requests = 0;
  server(t, async (url) => {
    requests++;
    assert.equal(url, `https://api.example.test/api/v1/recordings/${saved.id}`);
    return response({ recording: detailRecord });
  });

  const stop = flow("startRecordingDetailLifecycle")(store, saved.id, scheduler);
  await settle();
  assert.equal(requests, 1);
  assert.equal(store.getState().app.recordings[0].interviewTurns.length, 1);
  assert.match(renderDetails(store, saved.id), /Interviewer:<\/strong> Where did you go\?/);
  await scheduler.tick();
  assert.equal(requests, 1, "a ready hydrated detail must not keep polling");
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
