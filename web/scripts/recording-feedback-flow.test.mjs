import assert from "node:assert/strict";
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
const { shouldPollRecording } = load("src/lib/shadowing.ts");
const flows = load("src/lib/routeFlows.ts");
const initial = () => app.default(undefined, { type: "test/init" });
const recording = {
  id: "feedback-recording", topic: "Travel", duration: 20, timestamp: "2026-09-30T10:00:00Z",
  practiceType: "free_talk", status: "ready", transcript: "I went home.", correctedTranscript: "I went home.",
  interviewTurns: [], media: null, processingStage: null, processingError: null,
  shadowingStatus: "failed", shadowingError: "Provider failed.", shadowingUpdatedAt: "2026-09-30T10:00:00Z",
};
const storeFor = (overrides = {}) => configureStore({ reducer: { app: app.default }, preloadedState: { app: { ...initial(), isAuthenticated: true, ...overrides, recordings: [{ ...recording }] } } });
const deferred = () => { let resolve; const promise = new Promise(yes => { resolve = yes; }); return { promise, resolve }; };
const response = (record, status = 200) => new Response(JSON.stringify({ recording: record }), { status });
const server = (t, handler) => { t.mock.method(globalThis, "fetch", handler); api.configureApiClient("https://api.example.test"); api.configureApiAuthorization(null); };
const renderDetails = (record, overrides = {}) => {
  const store = configureStore({ reducer: { app: app.default }, preloadedState: { app: {
    ...initial(), isAuthenticated: true, ...overrides, recordings: [{ ...recording, interviewTurns: [], media: null, ...record }],
  } } });
  return renderToStaticMarkup(createElement(Provider, { store }, createElement(AppRouterContext.Provider, { value: {} },
    createElement(load("src/components/DetailsScreen.tsx").default, { recordingId: recording.id }))));
};

for (const staleResponse of ["success", "failure"]) {
  test(`late GET ${staleResponse} cannot undo shadowing retry or stop polling`, async t => {
    const store = storeFor(), old = deferred();
    server(t, async (_url, init) => init.method === "POST" ? response({ ...recording, shadowingStatus: "processing" }) : old.promise);
    const read = store.dispatch(app.fetchRecording(recording.id));
    await store.dispatch(app.generateShadowingAudio(recording.id)).unwrap();
    old.resolve(staleResponse === "success" ? response(recording) : response(null, 503));
    await read;
    const current = store.getState().app.recordings[0];
    assert.equal(current.shadowingStatus, "processing");
    assert.equal(shouldPollRecording(current.status, current.shadowingStatus), true);
    assert.equal(flows.recordingDetailState(store.getState().app, recording.id).error, null);
  });
}

test("a late history response cannot overwrite a retry or a more recent detail read", async t => {
  const store = storeFor(), history = deferred();
  server(t, async (url, init) => {
    if (url.endsWith("/profile")) return new Response(JSON.stringify({ interestIds: [], englishLevel: "B1" }));
    if (url.includes("?limit=100")) return history.promise;
    return response({ ...recording, shadowingStatus: init.method === "POST" ? "processing" : "ready" });
  });
  const read = store.dispatch(app.fetchUserData());
  await store.dispatch(app.generateShadowingAudio(recording.id)).unwrap();
  await store.dispatch(app.fetchRecording(recording.id)).unwrap();
  history.resolve(new Response(JSON.stringify({ items: [{ ...recording, shadowingStatus: "failed" }] })));
  await read.unwrap();
  assert.equal(store.getState().app.recordings[0].shadowingStatus, "ready");
});

test("a mutation completed after session expiry cannot restore old account data", async t => {
  const store = storeFor(), pending = deferred();
  server(t, async () => pending.promise);
  const attempt = store.dispatch(app.generateShadowingAudio(recording.id));
  store.dispatch(app.fetchRecording.rejected(new Error("Unauthorized"), "expired", recording.id, "Unauthorized", { failureKind: "unauthorized" }));
  pending.resolve(response({ ...recording, shadowingStatus: "processing" }));
  await attempt;
  assert.equal(store.getState().app.isAuthenticated, false);
  assert.equal(store.getState().app.recordings.length, 0);
});

test("failure on a previous recording does not end another recording's shadowing request", async t => {
  const store = storeFor(), previous = deferred(), current = deferred();
  server(t, async url => url.includes("previous-recording") ? previous.promise : current.promise);
  const first = store.dispatch(app.generateShadowingAudio("previous-recording"));
  const second = store.dispatch(app.generateShadowingAudio(recording.id));
  previous.resolve(response(null, 503));
  await first;
  assert.equal(store.getState().app.shadowingRequestStatus, "loading");
  assert.equal(store.getState().app.shadowingRequestError, null);
  current.resolve(response({ ...recording, shadowingStatus: "processing" }));
  await second;
});

const focusedFeedback = { version: 1, answers: [{ items: [{
  id: "past", kind: "blocker", originalFragment: "go", correctedFragment: "went", occurrence: 1,
  span: { start: 2, end: 4 }, title: "Вы говорили о прошлом", explanation: "Используйте went.", ruleId: "past-simple",
  practiceText: "She went to the library yesterday.",
}] }] };

test("missing or invalid modern feedback offers analysis without old corrections or shadowing", async t => {
  const old = { ...recording, transcript: "I go home.", suggestions: [{ wrong: "go", right: "went", explanation: "Obsolete correction." }],
    strengths: [{ excerpt: "home", explanation: "Obsolete praise." }], strengthsStatus: "processing", shadowingScript: { text: "Obsolete sample." },
    focusedFeedback: { version: 99, answers: [] } };
  const store = storeFor();
  server(t, async () => response(old));
  await store.dispatch(app.fetchRecording(recording.id)).unwrap();
  const parsed = store.getState().app.recordings[0];
  for (const key of ["suggestions", "strengths", "strengthsStatus", "shadowingScript"]) assert.equal(Object.hasOwn(parsed, key), false);
  assert.equal(parsed.focusedFeedback, undefined);
  const markup = renderDetails(parsed);
  assert.match(markup, /Сделать разбор/);
  assert.match(markup, /I go home\./);
  assert.doesNotMatch(markup, /Obsolete|Corrections and strengths|transcript-error-mark|Shadowing practice|Исправить свои ответы/);
  assert.equal(shouldPollRecording(parsed.status, parsed.shadowingStatus), false);
});

test("empty focused analysis is complete while processing, failed, and missing results show distinct actions", () => {
  const empty = renderDetails({ focusedFeedback: { version: 1, answers: [{ items: [] }] } });
  assert.doesNotMatch(empty, /Сделать разбор|Разбор ещё не готов/);
  assert.match(empty, /Shadowing practice/);
  const complete = renderDetails({ transcript: "I go home.", focusedFeedback });
  assert.match(complete, /focus-mark focus-blocker/);
  assert.doesNotMatch(complete, /Сделать разбор/);
  const processing = renderDetails({ status: "processing", processingStage: "suggestions" });
  assert.doesNotMatch(processing, /Сделать разбор/);
  assert.match(processing, /Analyzing your English/);
  const failed = renderDetails({ status: "failed", processingStage: "suggestions" });
  assert.match(failed, /Retry AI analysis/);
  assert.doesNotMatch(failed, /Сделать разбор/);
  assert.doesNotMatch(renderDetails({ transcript: "" }), /Сделать разбор/);
  assert.match(renderDetails({}, { recordingFeedbackStatuses: { [recording.id]: "loading" } }), /disabled=""[^>]*>Запускаем разбор/);
  assert.match(renderDetails({}, { recordingFeedbackErrors: { [recording.id]: "Try again." } }), /Повторить разбор[\s\S]*role="alert">Try again/);
});

test("analysis is single-flight, clears old results, and polls GET until the focused result is ready", async t => {
  const store = storeFor(), post = deferred(), requests = [];
  server(t, async (url, init) => {
    requests.push([url, init.method ?? "GET"]);
    if (init.method === "POST") {
      assert.match(JSON.parse(init.body).idempotencyKey, /^feedback:/);
      return post.promise;
    }
    return response({ ...recording, transcript: "I go home.", focusedFeedback });
  });
  const start = store.dispatch(app.requestRecordingFeedback(recording.id));
  const duplicate = await store.dispatch(app.requestRecordingFeedback(recording.id));
  assert.equal(duplicate.meta.condition, true);
  assert.equal(requests.length, 1);
  post.resolve(new Response(JSON.stringify({ scheduled: true }), { status: 202 }));
  await start.unwrap();
  const waiting = store.getState().app.recordings[0];
  assert.equal(waiting.status, "processing");
  assert.equal(waiting.processingStage, "suggestions");
  assert.equal(waiting.correctedTranscript, "");
  assert.equal(waiting.shadowingStatus, "pending");
  assert.equal(shouldPollRecording(waiting.status, waiting.shadowingStatus), true);
  await store.dispatch(app.fetchRecording(recording.id)).unwrap();
  const ready = store.getState().app.recordings[0];
  assert.equal(ready.status, "ready");
  assert.equal(ready.focusedFeedback.answers[0].items[0].correctedFragment, "went");
  assert.equal(shouldPollRecording(ready.status, ready.shadowingStatus), false);
  assert.deepEqual(requests.map(([, method]) => method), ["POST", "GET"]);
});

test("retries reuse their key after a lost response and keep failures visible", async t => {
  const store = storeFor(), keys = [];
  server(t, async (_url, init) => {
    keys.push(JSON.parse(init.body).idempotencyKey);
    if (keys.length === 1) throw new Error("Connection lost after acceptance.");
    if (keys.length === 2) return new Response(JSON.stringify({ error: { message: "Temporary service failure." } }), { status: 503 });
    return new Response(JSON.stringify({ scheduled: false }), { status: 202 });
  });
  await assert.rejects(store.dispatch(app.requestRecordingFeedback(recording.id)).unwrap());
  assert.equal(store.getState().app.recordingFeedbackStatuses[recording.id], undefined);
  assert.ok(store.getState().app.recordingFeedbackErrors[recording.id]);
  await assert.rejects(store.dispatch(app.requestRecordingFeedback(recording.id)).unwrap());
  assert.equal(store.getState().app.recordingFeedbackErrors[recording.id], "Temporary service failure.");
  await store.dispatch(app.requestRecordingFeedback(recording.id)).unwrap();
  assert.equal(new Set(keys).size, 1);
  assert.equal(store.getState().app.recordingFeedbackErrors[recording.id], undefined);
  assert.equal(store.getState().app.recordings[0].status, "processing");
});

for (const action of ["expiry", "deletion"]) {
  test(`feedback completion after ${action} cannot restore a recording`, async t => {
    const store = storeFor(), pending = deferred();
    server(t, async () => pending.promise);
    const start = store.dispatch(app.requestRecordingFeedback(recording.id));
    if (action === "expiry") store.dispatch(app.fetchRecording.rejected(new Error("Unauthorized"), "expired", recording.id, "Unauthorized", { failureKind: "unauthorized" }));
    else store.dispatch(app.deleteRecording.fulfilled({ recordingId: recording.id, quota: null }, "delete", recording.id));
    pending.resolve(new Response(JSON.stringify({ scheduled: true }), { status: 202 }));
    await start;
    assert.equal(store.getState().app.recordings.length, 0);
    assert.equal(Object.keys(store.getState().app.recordingFeedbackRequestKeys).length, 0);
  });
}

test("late GET cannot overwrite an accepted analysis request", async t => {
  const store = storeFor(), old = deferred();
  server(t, async (_url, init) => init.method === "POST"
    ? new Response(JSON.stringify({ scheduled: true }), { status: 202 }) : old.promise);
  const read = store.dispatch(app.fetchRecording(recording.id));
  await store.dispatch(app.requestRecordingFeedback(recording.id)).unwrap();
  old.resolve(response(recording));
  await read;
  assert.equal(store.getState().app.recordings[0].status, "processing");
});

test("completed, processing, local, unauthorized and transcript-free recordings never POST analysis", async t => {
  let calls = 0;
  server(t, async () => { calls++; throw new Error("Must not call server"); });
  for (const overrides of [{ focusedFeedback }, { status: "processing" }, { status: "failed" }, { id: "local-draft" }, { transcript: " " }]) {
    const store = configureStore({ reducer: { app: app.default }, preloadedState: { app: { ...initial(), isAuthenticated: true, recordings: [{ ...recording, ...overrides }] } } });
    assert.equal((await store.dispatch(app.requestRecordingFeedback(overrides.id ?? recording.id))).meta.condition, true);
  }
  const unauthenticated = configureStore({ reducer: { app: app.default } });
  assert.equal((await unauthenticated.dispatch(app.requestRecordingFeedback(recording.id))).meta.condition, true);
  assert.equal(calls, 0);
});
