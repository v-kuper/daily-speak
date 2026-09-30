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
  suggestions: [], strengths: [], strengthsStatus: "failed", processingStage: null, processingError: null,
  shadowingStatus: "failed", shadowingError: "Provider failed.", shadowingUpdatedAt: "2026-09-30T10:00:00Z",
};
const storeFor = () => configureStore({ reducer: { app: app.default }, preloadedState: { app: { ...initial(), isAuthenticated: true, recordings: [{ ...recording }] } } });
const deferred = () => { let resolve; const promise = new Promise(yes => { resolve = yes; }); return { promise, resolve }; };
const response = (record, status = 200) => new Response(JSON.stringify({ recording: record }), { status });
const server = (t, handler) => { t.mock.method(globalThis, "fetch", handler); api.configureApiClient("https://api.example.test"); api.configureApiAuthorization(null); };
const renderDetails = (record) => {
  const store = configureStore({ reducer: { app: app.default }, preloadedState: { app: {
    ...initial(), isAuthenticated: true, recordings: [{ ...recording, interviewTurns: [], media: null, ...record }],
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
    assert.equal(shouldPollRecording(current.status, current.shadowingStatus, current.strengthsStatus), true);
    assert.equal(flows.recordingDetailState(store.getState().app, recording.id).error, null);
  });
}

test("independent mutation responses preserve each other's progress", async t => {
  const store = storeFor(), positive = deferred();
  server(t, async url => url.endsWith("/strengths") ? positive.promise : response({ ...recording, shadowingStatus: "processing" }));
  const strengths = store.dispatch(app.retryRecordingStrengths(recording.id));
  await store.dispatch(app.generateShadowingAudio(recording.id)).unwrap();
  positive.resolve(response({ ...recording, strengthsStatus: "processing" }));
  await strengths.unwrap();
  assert.equal(store.getState().app.recordings[0].shadowingStatus, "processing");
  assert.equal(store.getState().app.recordings[0].strengthsStatus, "processing");
});

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

test("good-example completion keeps detail polling alive after shadowing is ready", async t => {
  const store = storeFor();
  server(t, async () => response({ ...recording, shadowingStatus: "ready", strengthsStatus: "processing" }));
  await store.dispatch(app.retryRecordingStrengths(recording.id)).unwrap();
  const current = store.getState().app.recordings[0];
  assert.equal(shouldPollRecording("ready", "ready", current.strengthsStatus), true);
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

test("detail feedback distinguishes processing, failure, checked-empty, and legacy coverage", () => {
  assert.match(renderDetails({ strengthsStatus: "processing" }), /Finding useful examples/);
  assert.match(renderDetails({ strengthsStatus: "failed" }), /Good examples could not be checked.*Your corrections are saved/);
  assert.match(renderDetails({ strengthsStatus: "failed" }), /Retry good examples/);
  const checked = renderDetails({ strengthsStatus: "ready" });
  assert.match(checked, /No specific examples were selected this time/);
  assert.doesNotMatch(checked, /Retry good examples/);
  assert.match(renderDetails({ strengthsStatus: "unknown" }), /Find good examples/);
});

test("detail cards use stable anchor IDs and hide unreachable transcript links", () => {
  const corrections = [{ id: "specific-go", wrong: "I go", right: "I went", explanation: "Use past simple for yesterday.", span: { start: 26, end: 30 } }];
  const markup = renderDetails({ transcript: "I go every day. Yesterday I go.", suggestions: corrections });
  assert.match(markup, /id="feedback-correction-specific-go"/);
  assert.match(markup, /aria-controls="feedback-correction-specific-go"/);
  assert.match(markup, /Show in transcript/);
  const ambiguous = renderDetails({ transcript: "I go every day. Yesterday I go.", suggestions: [{ ...corrections[0], span: undefined }] });
  assert.doesNotMatch(ambiguous, /Show in transcript/);
  assert.match(ambiguous, /Use past simple for yesterday/);
  assert.match(markup, /Ваш ответ в естественной форме/);
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
