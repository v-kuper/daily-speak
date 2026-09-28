import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const sourcePath = (relativePath) => fileURLToPath(new URL(relativePath, import.meta.url));
const {
  advanceInterviewTimeline,
  MAX_LIVE_SEGMENT_ATTEMPTS,
  rotateFailedInterviewSegment,
  withoutInterviewTimeline,
} = load(sourcePath("../src/lib/interviewFlow.ts"));
const { encodeWav, InterviewTurnCapture } = load(sourcePath("../src/lib/interviewTurnCapture.ts"));
const { parseInterviewTurns } = load(sourcePath("../src/lib/interviewTimeline.ts"));
const { createInterviewRecovery, mayAbandonInterview, recoverPreviousInterview } = load(sourcePath("../src/lib/interviewRecovery.ts"));

const memoryStorage = () => {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
};

const recoveryRecord = (phase = "active", sessionId = "interview-1") => ({
  principalId: "owner-1",
  kind: "user",
  createKey: "stable-create-key",
  input: { topic: "Travel", level: "b1", interestIds: ["travel"] },
  sessionId,
  phase,
});

test("navigation cancels an unsaved interview but protects an ongoing or completed save", () => {
  assert.equal(mayAbandonInterview("interview-1", false, null, "active"), true);
  assert.equal(mayAbandonInterview("interview-1", true, null, "active"), false);
  assert.equal(mayAbandonInterview("interview-1", false, "interview-1", undefined), false);
  assert.equal(mayAbandonInterview("interview-1", false, null, "saving"), false);
  assert.equal(mayAbandonInterview(null, false, null), true, "pending preparation can still be abandoned");
});

test("a refreshed tab closes only its own abandoned interview before a new create", async () => {
  const tabStorage = memoryStorage();
  const sharedStorage = memoryStorage();
  const oldPage = createInterviewRecovery(tabStorage, sharedStorage, "old-page", () => 1000);
  oldPage.write(recoveryRecord());
  assert.equal(oldPage.claim("owner-1"), true);
  oldPage.release("owner-1");
  const newPage = createInterviewRecovery(tabStorage, sharedStorage, "new-page", () => 1001);
  const cancelled = [];
  await recoverPreviousInterview(newPage, { principalId: "owner-1", kind: "user" }, {
    rehydrate: async () => { throw new Error("A stored ID should not be created again."); },
    get: async () => ({ id: "interview-1", status: "recording" }),
    cancel: async (id) => { cancelled.push(id); },
  }, () => true, () => 1001);
  assert.deepEqual(cancelled, ["interview-1"]);
  assert.equal(newPage.read(), null);
});

test("a copied tab cannot cancel a live interview owned by another tab", async () => {
  const tabStorage = memoryStorage();
  const sharedStorage = memoryStorage();
  const firstTab = createInterviewRecovery(tabStorage, sharedStorage, "live-page", () => 1000);
  firstTab.write(recoveryRecord());
  assert.equal(firstTab.claim("owner-1"), true);
  const copiedTab = createInterviewRecovery(tabStorage, sharedStorage, "copied-page", () => 1001);
  let cancellationCalls = 0;
  await assert.rejects(recoverPreviousInterview(copiedTab, { principalId: "owner-1", kind: "user" }, {
    rehydrate: async () => ({ id: "interview-1", status: "ready" }),
    get: async () => ({ id: "interview-1", status: "recording" }),
    cancel: async () => { cancellationCalls += 1; },
  }, () => true, () => 1001), /another tab/);
  assert.equal(cancellationCalls, 0);
  assert.equal(copiedTab.read()?.sessionId, "interview-1");
});

test("recovery leaves a saving interview alone and clears one already linked", async () => {
  const tabStorage = memoryStorage();
  const sharedStorage = memoryStorage();
  const oldPage = createInterviewRecovery(tabStorage, sharedStorage, "old-page", () => 1000);
  oldPage.write(recoveryRecord("saving"));
  const newPage = createInterviewRecovery(tabStorage, sharedStorage, "new-page", () => 1001);
  let cancellationCalls = 0;
  const services = {
    rehydrate: async () => ({ id: "interview-1", status: "recording" }),
    get: async () => ({ id: "interview-1", status: "recording" }),
    cancel: async () => { cancellationCalls += 1; },
  };
  await assert.rejects(recoverPreviousInterview(newPage, { principalId: "owner-1", kind: "user" }, services, () => true, () => 1001), /still be saving/);
  assert.equal(cancellationCalls, 0);
  assert.equal(newPage.read()?.phase, "saving");
  await recoverPreviousInterview(newPage, { principalId: "owner-1", kind: "user" }, {
    ...services,
    get: async () => ({ id: "interview-1", status: "finalizing" }),
  }, () => true, () => 1002);
  assert.equal(cancellationCalls, 0);
  assert.equal(newPage.read(), null);
});

test("a stalled save can be abandoned after the previous page is gone", async () => {
  const tabStorage = memoryStorage();
  const sharedStorage = memoryStorage();
  createInterviewRecovery(tabStorage, sharedStorage, "old-page", () => 1000).write(recoveryRecord("saving"));
  const newPage = createInterviewRecovery(tabStorage, sharedStorage, "new-page", () => 31_001);
  let cancelled = false;
  await recoverPreviousInterview(newPage, { principalId: "owner-1", kind: "user" }, {
    rehydrate: async () => ({ id: "interview-1", status: "recording" }),
    get: async () => ({ id: "interview-1", status: "recording" }),
    cancel: async () => { cancelled = true; },
  }, () => true, () => 31_001);
  assert.equal(cancelled, true);
  assert.equal(newPage.read(), null);
});

test("recovery stops after a generation change and never cancels another principal", async () => {
  const tabStorage = memoryStorage();
  const sharedStorage = memoryStorage();
  const oldPage = createInterviewRecovery(tabStorage, sharedStorage, "old-page", () => 1000);
  oldPage.write(recoveryRecord("preparing", null));
  const newPage = createInterviewRecovery(tabStorage, sharedStorage, "new-page", () => 1001);
  let current = true;
  let cancellationCalls = 0;
  const services = {
    rehydrate: async () => { current = false; return { id: "interview-1", status: "preparing" }; },
    get: async () => ({ id: "interview-1", status: "preparing" }),
    cancel: async () => { cancellationCalls += 1; },
  };
  await recoverPreviousInterview(newPage, { principalId: "owner-1", kind: "user" }, services, () => current, () => 1001);
  assert.equal(cancellationCalls, 0);
  assert.equal(newPage.read()?.createKey, "stable-create-key");
  await recoverPreviousInterview(newPage, { principalId: "different-owner", kind: "user" }, services, () => true, () => 1002);
  assert.equal(cancellationCalls, 0);
  assert.equal(newPage.read(), null);
});

test("Next keeps the current answer open when no question is ready", () => {
  const session = {
    id: "session-1", status: "recording", topic: "Travel", usefulWords: [],
    turns: [{ seq: 1, question: "Travel", askedAtMs: 0, endedAtMs: null, provisionalTranscript: "" }],
    candidates: [], currentTurnSeq: 1, maxDurationSeconds: 60,
  };
  assert.equal(advanceInterviewTimeline(session, new Set(), 5000), null);
  assert.equal(session.turns[0].endedAtMs, null);
  session.candidates = [{ id: "ready-1", question: "Where would you go first?" }];
  assert.equal(advanceInterviewTimeline(session, new Set(), 100), null, "rapid Next must not create an empty answer");
  assert.equal(advanceInterviewTimeline(session, new Set(), 5000, 5000), null, "Next cannot open a question at the session limit");
  const advance = advanceInterviewTimeline(session, new Set(), 5000);
  assert.equal(advance.session.turns[0].endedAtMs, 5000);
  assert.equal(advance.session.turns[1].askedAtMs, 5000);
  assert.equal(advance.session.turns[1].question, "Where would you go first?");
  assert.equal(session.turns[0].endedAtMs, null);
});

test("a failed live WAV rotates behind later answers and is eventually dropped", () => {
  const first = { seq: 1, attempts: 0 };
  const second = { seq: 2, attempts: 0 };
  let result = rotateFailedInterviewSegment([first, second], first, MAX_LIVE_SEGMENT_ATTEMPTS);
  assert.equal(result.dropped, false);
  assert.deepEqual(result.queue.map(({ seq, attempts }) => ({ seq, attempts })), [
    { seq: 2, attempts: 0 },
    { seq: 1, attempts: 1 },
  ]);

  result = rotateFailedInterviewSegment(result.queue, result.queue[1], MAX_LIVE_SEGMENT_ATTEMPTS);
  result = rotateFailedInterviewSegment(result.queue, result.queue[1], MAX_LIVE_SEGMENT_ATTEMPTS);
  assert.equal(result.dropped, true);
  assert.deepEqual(result.queue, [{ seq: 2, attempts: 0 }]);
});

test("degraded save preserves the full recording while omitting unsynchronized timeline linkage", () => {
  const plain = withoutInterviewTimeline({
    localRecordingId: "local-1",
    topic: "Travel",
    duration: 42,
    audioDataUrl: "data:audio/webm;base64,AAA=",
    interviewSessionId: "interview-1",
    interviewEndedAtMs: 42_000,
  });
  assert.deepEqual(plain, {
    localRecordingId: "local-1",
    topic: "Travel",
    duration: 42,
    audioDataUrl: "data:audio/webm;base64,AAA=",
  });
});

test("saved interview timeline labels approximate final answer matching", () => {
  const turns = parseInterviewTurns([{
    sequence: 1, question: "Where did you go?", askedAtMs: 0, endedAtMs: 3000,
    answerText: "I went to Rome.", answerSource: "final", answerAlignment: "approximate",
  }]);
  assert.equal(turns[0].answerSource, "final");
  assert.equal(turns[0].answerAlignment, "approximate");
});

test("interview create retries use the same key in body and cancellation uses the owner session", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify({ interview: {
      id: "interview-1", status: requests.length === 1 ? "preparing" : "cancelled",
      topic: "My dog", openingQuestion: "What would you like to share about your dog?",
      usefulWords: [], candidates: [], turns: [], currentTurnSeq: null, maxDurationSeconds: 60,
    } }), { status: requests.length === 1 ? 202 : 200, headers: { "Content-Type": "application/json" } });
  };
  try {
    const { configureApiClient } = load(sourcePath("../src/lib/apiClient.ts"));
    const { prepareInterview, cancelInterview } = load(sourcePath("../src/lib/interviewSession.ts"));
    configureApiClient("https://example.test");
    const prepared = await prepareInterview({ topic: "My dog", level: "b1", interestIds: [], guest: false, idempotencyKey: "stable-prepare-key" });
    assert.equal(prepared.openingQuestion, "What would you like to share about your dog?");
    assert.equal(JSON.parse(requests[0].init.body).idempotencyKey, "stable-prepare-key");
    assert.equal(JSON.parse(requests[0].init.body).englishLevel, "b1");
    await cancelInterview(prepared.id, true);
    assert.ok(requests[1].url.endsWith("/api/v1/interviews/interview-1/cancel"));
    assert.equal(requests[1].init.keepalive, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("turn WAV upload binds the media asset to its interview session", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    const path = new URL(String(url)).pathname;
    requests.push({ path, init });
    if (path === "/api/v1/media/uploads") {
      return new Response(JSON.stringify({
        asset: { id: "answer-asset", state: "ready" },
        upload: { id: "answer-upload", state: "completed", partSizeBytes: 1024, partCount: 1 },
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (path === "/api/v1/interviews/interview-1/turns/1/audio") {
      return new Response(null, { status: 202 });
    }
    throw new Error(`Unexpected request: ${path}`);
  };
  try {
    const { configureApiClient } = load(sourcePath("../src/lib/apiClient.ts"));
    const { uploadInterviewTurnAudio } = load(sourcePath("../src/lib/interviewSession.ts"));
    configureApiClient("https://example.test");
    await uploadInterviewTurnAudio("interview-1", 1, new Blob([new Uint8Array([1, 2])], { type: "audio/wav" }), false, "turn-key");
    assert.equal(requests.length, 2);
    const mediaBody = JSON.parse(requests[0].init.body);
    assert.equal(mediaBody.purpose, "interview_turn_audio");
    assert.equal(mediaBody.interviewSessionId, "interview-1");
    assert.equal(JSON.parse(requests[1].init.body).audioAssetId, "answer-asset");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("answer PCM is wrapped in a valid mono WAV header", async () => {
  const blob = encodeWav([new Int16Array([0, 32767]), new Int16Array([-32768, 1])], 16000);
  const bytes = new DataView(await blob.arrayBuffer());
  const label = (offset, length) => String.fromCharCode(...new Uint8Array(bytes.buffer, offset, length));
  assert.equal(blob.type, "audio/wav");
  assert.equal(blob.size, 52);
  assert.equal(label(0, 4), "RIFF");
  assert.equal(label(8, 4), "WAVE");
  assert.equal(bytes.getUint16(22, true), 1);
  assert.equal(bytes.getUint32(24, true), 16000);
  assert.equal(bytes.getUint16(34, true), 16);
  assert.equal(label(36, 4), "data");
  assert.equal(bytes.getUint32(40, true), 8);
  assert.equal(bytes.getInt16(48, true), -32768);
});

test("a late answer boundary cannot leak stale PCM into the next WAV", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = {
    setTimeout,
    clearTimeout,
  };
  const sent = [];
  const processor = { port: { onmessage: null, postMessage: (message) => sent.push(message) } };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000 },
      { disconnect() {} },
      processor,
      { disconnect() {} },
    );
    processor.port.onmessage({ data: { type: "samples", pcm: new Int16Array([111]) } });
    processor.port.onmessage({ data: { type: "boundary", id: 999 } });
    const next = capture.closeTurn();
    assert.deepEqual(sent, [{ type: "boundary", id: 1 }]);
    processor.port.onmessage({ data: { type: "samples", pcm: new Int16Array([222]) } });
    processor.port.onmessage({ data: { type: "boundary", id: 1 } });
    const blob = await next;
    const wav = new DataView(await blob.arrayBuffer());
    assert.equal(blob.size, 46);
    assert.equal(wav.getInt16(44, true), 222);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("worklet closes each answer at its own boundary", () => {
  const messages = [];
  let Processor;
  class AudioWorkletProcessor {
    constructor() {
      this.port = { onmessage: null, postMessage: (message) => messages.push(message) };
    }
  }
  runInNewContext(readFileSync(resolve("public/interview-capture-worklet.js"), "utf8"), {
    AudioWorkletProcessor,
    registerProcessor: (_name, value) => { Processor = value; },
    sampleRate: 48000,
    Int16Array,
    Math,
  });
  const processor = new Processor();
  processor.process([[new Float32Array(128).fill(0.5)]]);
  processor.port.onmessage({ data: { type: "boundary", id: 1 } });
  processor.process([[new Float32Array(128).fill(-0.25)]]);
  processor.port.onmessage({ data: { type: "boundary", id: 2 } });
  assert.deepEqual(messages.filter((message) => message.type === "boundary").map((message) => message.id), [1, 2]);
  const pcm = messages.filter((message) => message.type === "samples").map((message) => message.pcm);
  assert.equal(pcm.length, 2);
  assert.ok(pcm[0].every((sample) => sample > 16000));
  assert.ok(pcm[1].every((sample) => sample < -8000));
});
