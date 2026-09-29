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
  closeInterviewTimeline,
  commitInterviewAdvance,
  hasInterviewAnswerEvidence,
  MAX_LIVE_SEGMENT_ATTEMPTS,
  resolveInterviewRecordingLimitSeconds,
  rotateFailedInterviewSegment,
} = load(sourcePath("../src/lib/interviewFlow.ts"));
const { encodeWav, InterviewTurnCapture, pcmHasSpeechActivity } = load(sourcePath("../src/lib/interviewTurnCapture.ts"));
const { mergeInterviewTranscriptStatus, parseInterviewVocabulary, preserveLiveInterviewTurn } = load(sourcePath("../src/lib/interviewSession.ts"));
const { buildQuestionSpeechRequest, QuestionSpeechPlayer } = load(sourcePath("../src/lib/questionSpeech.ts"));
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

test("server polling preserves local captions until a canonical transcript is ready", () => {
  const local = {
    seq: 1,
    question: "Where did you go?",
    askedAtMs: 0,
    endedAtMs: 2000,
    provisionalTranscript: "",
    transcriptStatus: "queued",
    liveTranscriptFinal: "I went ",
    liveTranscriptInterim: "home",
  };
  const pending = preserveLiveInterviewTurn({
    ...local,
    provisionalTranscript: "",
    liveTranscriptFinal: undefined,
    liveTranscriptInterim: undefined,
  }, local);
  assert.equal(pending.liveTranscriptFinal, "I went ");
  assert.equal(pending.liveTranscriptInterim, "home");

  const ready = preserveLiveInterviewTurn({
    ...local,
    provisionalTranscript: "I went home after batch transcription.",
    transcriptStatus: "ready",
    liveTranscriptFinal: undefined,
    liveTranscriptInterim: undefined,
  }, local);
  assert.equal(ready.provisionalTranscript, "I went home after batch transcription.");
  assert.equal(ready.liveTranscriptFinal, undefined);
  assert.equal(ready.liveTranscriptInterim, undefined);
  const readyEmpty = preserveLiveInterviewTurn({
    ...local,
    provisionalTranscript: "",
    transcriptStatus: "ready",
    liveTranscriptFinal: undefined,
    liveTranscriptInterim: undefined,
  }, { ...local, provisionalTranscript: "stale fallback text" });
  assert.equal(readyEmpty.provisionalTranscript, "");
  assert.equal(readyEmpty.liveTranscriptInterim, undefined);
  assert.equal(mergeInterviewTranscriptStatus("ready", "failed"), "ready");
  assert.equal(mergeInterviewTranscriptStatus("queued", "failed"), "failed");
  assert.equal(mergeInterviewTranscriptStatus("queued", "pending"), "queued");
});

test("interview preparation keeps translated vocabulary while older sessions fall back to English words", () => {
  assert.deepEqual(parseInterviewVocabulary([
    { word: "  book a room ", translation: " забронировать номер " },
    { word: "route", translation: "маршрут" },
  ], ["route", "ticket"]), [
    { word: "book a room", translation: "забронировать номер" },
    { word: "route", translation: "маршрут" },
    { word: "ticket", translation: "" },
  ]);
  assert.deepEqual(parseInterviewVocabulary(undefined, ["journey"]), [
    { word: "journey", translation: "" },
  ]);
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
    candidates: [], currentTurnSeq: 1, maxDurationSeconds: 180,
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

test("a prepared Next commits only after its audio boundary and preserves late subtitles", () => {
  const session = {
    id: "interview-1",
    status: "recording",
    topic: "Travel",
    openingQuestion: "Where did you go?",
    error: null,
    usefulWords: [],
    turns: [{
      seq: 1,
      question: "Where did you go?",
      askedAtMs: 0,
      endedAtMs: null,
      provisionalTranscript: "",
      liveTranscriptFinal: "I went",
      liveTranscriptInterim: "",
    }],
    candidates: [{ id: "candidate-2", question: "What did you enjoy?" }],
    currentTurnSeq: 1,
    maxDurationSeconds: 600,
  };
  const advance = advanceInterviewTimeline(session, new Set(), 1200);
  assert.ok(advance);
  assert.equal(session.turns.length, 1, "preparing Next must leave the visible question open");

  const withLateSubtitle = {
    ...session,
    turns: [{
      ...session.turns[0],
      liveTranscriptFinal: "I went to Rome.",
    }],
  };
  const committed = commitInterviewAdvance(withLateSubtitle, advance);
  assert.ok(committed);
  assert.equal(committed.turns.length, 2);
  assert.equal(committed.turns[0].endedAtMs, 1200);
  assert.equal(committed.turns[0].liveTranscriptFinal, "I went to Rome.");
  assert.equal(committed.turns[1].question, "What did you enjoy?");
  assert.deepEqual(committed.candidates, []);
});

test("skipping a question opens the prepared next question without keeping the unanswered turn", () => {
  const session = {
    id: "interview-1", status: "recording", topic: "Travel", usefulWords: [],
    turns: [{ seq: 1, question: "Where did you go?", askedAtMs: 0, endedAtMs: null, provisionalTranscript: "" }],
    candidates: [{ id: "candidate-2", question: "How do you usually travel?" }],
    currentTurnSeq: 1, maxDurationSeconds: 600,
  };
  const advance = advanceInterviewTimeline(session, new Set(), 1200, 600_000, true);
  assert.ok(advance);
  assert.equal(advance.skipPrevious, true);
  const committed = commitInterviewAdvance(session, advance);
  assert.deepEqual(committed.turns.map((turn) => turn.seq), [2]);
  assert.equal(committed.turns[0].question, "How do you usually travel?");
});

test("stopping on an unanswered final question removes only that turn", () => {
  const session = {
    id: "interview-1", status: "recording", topic: "Travel", usefulWords: [], candidates: [],
    turns: [
      { seq: 1, question: "Where did you go?", askedAtMs: 0, endedAtMs: 2000, provisionalTranscript: "Rome." },
      { seq: 2, question: "What did you enjoy?", askedAtMs: 2000, endedAtMs: null, provisionalTranscript: "" },
    ],
    currentTurnSeq: 2, maxDurationSeconds: 600,
  };
  const closed = closeInterviewTimeline(session, 5000, true);
  assert.equal(closed.skippedTurn?.seq, 2);
  assert.deepEqual(closed.session.turns.map((turn) => turn.seq), [1]);
  assert.equal(closed.session.currentTurnSeq, 1);
});

test("interview recording limits use the account cap and the server session cap", () => {
  assert.equal(resolveInterviewRecordingLimitSeconds({
    isAuthenticated: true,
    authenticatedLimitSeconds: 600,
    guestLimitSeconds: 180,
    interviewLimitSeconds: 600,
  }), 600);
  assert.equal(resolveInterviewRecordingLimitSeconds({
    isAuthenticated: false,
    authenticatedLimitSeconds: 600,
    guestLimitSeconds: 180,
    interviewLimitSeconds: 180,
  }), 180);
  assert.equal(resolveInterviewRecordingLimitSeconds({
    isAuthenticated: true,
    authenticatedLimitSeconds: 600,
    guestLimitSeconds: 180,
    interviewLimitSeconds: 300,
  }), 300, "the server session cap remains authoritative");
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

test("saved interview timeline labels approximate final answer matching", () => {
  const turns = parseInterviewTurns([{
    sequence: 1, question: "Where did you go?", askedAtMs: 0, endedAtMs: 3000,
    answerText: "I went to Rome.", answerSource: "final", answerAlignment: "approximate",
    correctedAnswerText: "I travelled to Rome.",
  }]);
  assert.equal(turns[0].answerSource, "final");
  assert.equal(turns[0].answerAlignment, "approximate");
  assert.equal(turns[0].correctedAnswerText, "I travelled to Rome.");
  assert.equal("correctedAnswerText" in parseInterviewTurns([{
    sequence: 2, question: "What next?", answerText: "Nothing.", answerSource: "final",
  }])[0], false);
});

test("interview create retries use the same key in body and cancellation uses the owner session", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return new Response(JSON.stringify({ interview: {
      id: "interview-1", status: requests.length === 1 ? "preparing" : "cancelled",
      topic: "My dog", openingQuestion: "What would you like to share about your dog?",
      usefulWords: [], candidates: [], turns: [], currentTurnSeq: null, maxDurationSeconds: 180,
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

test("question changes carry the explicit skipped-turn state", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    requests.push({ path: new URL(String(url)).pathname, init });
    return new Response(JSON.stringify({ interview: {
      id: "interview-1", status: "recording", topic: "Travel", openingQuestion: "Tell me about travel.",
      error: null, usefulWords: [], turns: [], candidates: [], currentTurnSeq: null, maxDurationSeconds: 600,
    } }), { status: 200, headers: { "Content-Type": "application/json" } });
  };
  try {
    const { configureApiClient } = load(sourcePath("../src/lib/apiClient.ts"));
    const { advanceInterview, skipInterviewTurn } = load(sourcePath("../src/lib/interviewSession.ts"));
    configureApiClient("https://example.test");
    await advanceInterview("interview-1", 1, "candidate-2", 1200, "advance-skip-1234", true);
    await skipInterviewTurn("interview-1", 2, 2400, "stop-skip-123456");
    assert.equal(requests[0].path, "/api/v1/interviews/interview-1/advance");
    assert.deepEqual(JSON.parse(requests[0].init.body), {
      idempotencyKey: "advance-skip-1234",
      currentTurnSeq: 1,
      nextCandidateId: "candidate-2",
      atMs: 1200,
      skipCurrent: true,
    });
    assert.equal(requests[1].path, "/api/v1/interviews/interview-1/turns/2/skip");
    assert.deepEqual(JSON.parse(requests[1].init.body), {
      idempotencyKey: "stop-skip-123456",
      atMs: 2400,
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("realtime token and final turn transcript use the v1 interview contracts", async () => {
  const requests = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    const path = new URL(String(url)).pathname;
    requests.push({ path, init });
    if (path === "/api/v1/interviews/interview-1/transcription-token") {
      return new Response(JSON.stringify({
        token: "browser-token",
        expiresAt: "2026-09-28T12:00:00Z",
        websocketUrl: "wss://api.cartesia.ai/stt/websocket?cartesia_version=2026-08-14",
        model: "ink-2",
        encoding: "pcm_s16le",
        sampleRate: 16000,
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (path === "/api/v1/interviews/interview-1/turns/2/transcript") {
      return new Response(JSON.stringify({ interview: {
        id: "interview-1",
        status: "recording",
        topic: "Travel",
        openingQuestion: "Tell me about travel.",
        error: null,
        usefulWords: [],
        turns: [{
          seq: 2,
          question: "Where did you go?",
          askedAtMs: 1000,
          endedAtMs: 3000,
          provisionalTranscript: "I went to Rome.",
          transcriptStatus: "ready",
        }],
        candidates: [],
        currentTurnSeq: 2,
        maxDurationSeconds: 600,
      } }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    throw new Error(`Unexpected request: ${path}`);
  };
  try {
    const { configureApiClient } = load(sourcePath("../src/lib/apiClient.ts"));
    const { getInterviewTranscriptionToken, submitInterviewTurnTranscript } = load(sourcePath("../src/lib/interviewSession.ts"));
    configureApiClient("https://example.test");
    const token = await getInterviewTranscriptionToken("interview-1");
    assert.equal(token.token, "browser-token");
    assert.equal(token.sampleRate, 16000);
    const interview = await submitInterviewTurnTranscript(
      "interview-1",
      2,
      "I went to Rome.",
      "turn-2:transcript",
    );
    assert.equal(interview.turns[0].provisionalTranscript, "I went to Rome.");
    assert.deepEqual(JSON.parse(requests[0].init.body), {});
    assert.deepEqual(JSON.parse(requests[1].init.body), {
      idempotencyKey: "turn-2:transcript",
      text: "I went to Rome.",
    });
    assert.equal(requests[1].init.headers["Idempotency-Key"], "turn-2:transcript");
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

test("answer evidence accepts live text or sustained PCM speech but rejects silence and spikes", () => {
  const turn = {
    seq: 1,
    question: "What happened?",
    askedAtMs: 0,
    endedAtMs: null,
    provisionalTranscript: "",
  };
  assert.equal(hasInterviewAnswerEvidence(turn, false), false);
  assert.equal(hasInterviewAnswerEvidence({ ...turn, liveTranscriptInterim: "I was" }, false), true);
  assert.equal(hasInterviewAnswerEvidence(turn, true), true);
  assert.equal(pcmHasSpeechActivity(new Int16Array(2048)), false);
  assert.equal(pcmHasSpeechActivity(new Int16Array(2048).fill(250)), false, "steady low-level noise is not speech");
  const spike = new Int16Array(2048);
  spike[100] = 20_000;
  assert.equal(pcmHasSpeechActivity(spike), false, "one microphone spike is not a spoken answer");
  const voiced = Int16Array.from({ length: 2048 }, (_, index) => Math.round(Math.sin(index / 8) * 2400));
  assert.equal(pcmHasSpeechActivity(voiced), true);
});

test("terminal answer failure tells the learner to re-record instead of promising another fallback", () => {
  const card = readFileSync(resolve("src/components/InterviewQuestionCard.tsx"), "utf8");
  const screen = readFileSync(resolve("src/components/SpeakScreen.tsx"), "utf8");
  assert.match(card, /could not be transcribed\. Re-record the interview before saving/);
  assert.match(card, /hasTerminalTranscriptionFailure/);
  assert.match(screen, /setInterviewCaptureFailure\(terminalMessage\)/);
});

test("the question card is the stable next control and allows an unanswered skip", () => {
  const card = readFileSync(resolve("src/components/InterviewQuestionCard.tsx"), "utf8");
  const screen = readFileSync(resolve("src/components/SpeakScreen.tsx"), "utf8");
  assert.match(card, /className="interview-question-card"/);
  assert.match(card, /onClick=\{onNext\}/);
  assert.doesNotMatch(card, /interview-next-btn|Next question →/);
  assert.match(card, /tap the question to skip it/i);
  assert.doesNotMatch(screen, /Say an answer before moving to the next question/);
});

test("question playback uses a TTS-only bearer token and native English settings", () => {
  const request = buildQuestionSpeechRequest({
    token: "short-lived-token",
    expiresAt: "2026-09-29T12:00:00Z",
    endpoint: "https://api.cartesia.ai/tts/bytes",
    apiVersion: "2026-08-14",
    model: "sonic-3.6",
    voiceId: "voice-1",
  }, "  How was your trip?  ");
  assert.equal(request.url, "https://api.cartesia.ai/tts/bytes");
  assert.equal(request.init.headers.Authorization, "Bearer short-lived-token");
  const body = JSON.parse(request.init.body);
  assert.equal(body.transcript, "How was your trip?");
  assert.equal(body.language, "en");
  assert.equal(body.model_id, "sonic-3.6");
  assert.equal(body.voice, "voice-1");
  assert.equal(body.output_format.container, "mp3");
  assert.throws(() => buildQuestionSpeechRequest({
    token: "token", expiresAt: "later", endpoint: "http://unsafe.test/tts",
    apiVersion: "version", model: "model", voiceId: "voice",
  }, "Question?"), /unavailable/);
});

test("question playback generates identical audio once and replays the cached bytes", async (t) => {
  const previousAudioContext = globalThis.AudioContext;
  class AudioContextFake {
    destination = {};
    resume() { return Promise.resolve(); }
    close() { return Promise.resolve(); }
    decodeAudioData() { return Promise.resolve({}); }
    createBufferSource() {
      return {
        buffer: null,
        onended: null,
        connect() {},
        disconnect() {},
        stop() {},
        start() { queueMicrotask(() => this.onended?.()); },
      };
    }
  }
  globalThis.AudioContext = AudioContextFake;
  t.after(() => {
    if (previousAudioContext === undefined) delete globalThis.AudioContext;
    else globalThis.AudioContext = previousAudioContext;
  });

  const player = new QuestionSpeechPlayer();
  let resolveAudio;
  let generations = 0;
  const load = () => {
    generations += 1;
    return new Promise((resolve) => { resolveAudio = resolve; });
  };
  const first = player.play("How was your trip?", load, () => undefined);
  await new Promise((resolve) => setImmediate(resolve));
  player.stop();
  const second = player.play("  How was   your trip? ", load, () => undefined);
  assert.equal(generations, 1, "a second click must share the in-flight generation");
  resolveAudio(new Uint8Array([1, 2, 3]).buffer);
  await Promise.all([first, second]);
  await player.play("How was your trip?", load, () => undefined);
  assert.equal(generations, 1, "a replay must use the cached audio bytes");
  player.dispose();
});

test("a delayed answer boundary remains queueable without leaking PCM into the next WAV", async () => {
  const originalWindow = globalThis.window;
  const delayedCallbacks = new Map();
  let nextTimer = 1;
  globalThis.window = {
    setTimeout: (callback) => {
      const id = nextTimer++;
      delayedCallbacks.set(id, callback);
      return id;
    },
    clearTimeout: (id) => delayedCallbacks.delete(id),
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
    const streamed = [];
    let delayed = 0;
    capture.setPCMListener((pcm) => streamed.push(...pcm));
    const next = capture.closeTurn(async () => "final transcript", () => { delayed += 1; });
    assert.deepEqual(sent, [{ type: "boundary", id: 1 }]);
    delayedCallbacks.get(1)();
    assert.equal(delayed, 1);
    let settled = false;
    void next.then(() => { settled = true; });
    await Promise.resolve();
    assert.equal(settled, false, "the delay notice must not reject or discard the pending partition");
    processor.port.onmessage({ data: { type: "samples", pcm: new Int16Array([222]) } });
    processor.port.onmessage({ data: { type: "boundary", id: 1 } });
    const captured = await next;
    const blob = captured.blob;
    assert.ok(blob);
    const wav = new DataView(await blob.arrayBuffer());
    assert.equal(blob.size, 48);
    assert.equal(wav.getInt16(44, true), 111);
    assert.equal(wav.getInt16(46, true), 222);
    assert.deepEqual(streamed, [222]);
    assert.equal(await captured.transcript, "final transcript");
  } finally {
    globalThis.window = originalWindow;
  }
});

test("PCM speech activity is scoped to one answer partition and resets at its boundary", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout };
  const activity = [];
  const processor = {
    port: { onmessage: null, postMessage() {} },
    addEventListener() {},
    removeEventListener() {},
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000 },
      { disconnect() {} },
      processor,
      { disconnect() {} },
      undefined,
      (active) => activity.push(active),
    );
    const voiced = Int16Array.from({ length: 2048 }, (_, index) => Math.round(Math.sin(index / 8) * 2400));
    processor.port.onmessage({ data: { type: "samples", pcm: voiced } });
    assert.equal(capture.hasSpeechActivity(), true);
    const first = capture.closeTurn();
    processor.port.onmessage({ data: { type: "boundary", id: 1 } });
    await first;
    assert.equal(capture.hasSpeechActivity(), false);
    processor.port.onmessage({ data: { type: "samples", pcm: new Int16Array(2048) } });
    assert.deepEqual(activity, [true, false]);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("an answer boundary timeout becomes terminal exactly once and ignores a late acknowledgement", async () => {
  const originalWindow = globalThis.window;
  const timers = new Map();
  let nextTimer = 1;
  globalThis.window = {
    setTimeout: (callback) => {
      const id = nextTimer++;
      timers.set(id, callback);
      return id;
    },
    clearTimeout: (id) => timers.delete(id),
  };
  const failures = [];
  let finalized = 0;
  const processor = {
    port: { onmessage: null, postMessage() {} },
    addEventListener() {},
    removeEventListener() {},
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000, close: async () => {} },
      { disconnect() {} },
      processor,
      { disconnect() {} },
      (error) => failures.push(error.message),
      undefined,
      1,
      2,
    );
    const lateHandler = processor.port.onmessage;
    const pending = capture.closeTurn(() => {
      finalized += 1;
      return Promise.resolve("late transcript");
    });
    const rejection = assert.rejects(pending, /did not finish its boundary/);
    timers.get(1)();
    await rejection;
    assert.equal(processor.port.onmessage, null);
    assert.equal(finalized, 0);
    assert.equal(failures.length, 1);
    lateHandler({ data: { type: "boundary", id: 1 } });
    assert.equal(finalized, 0);
    assert.equal(failures.length, 1);
    await assert.rejects(capture.closeTurn(), /did not finish its boundary/);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("final stop resumes a suspended audio context before posting its bounded boundary", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout };
  const events = [];
  const context = {
    sampleRate: 16000,
    state: "suspended",
    resume: async () => {
      events.push("resume");
      context.state = "running";
    },
    close: async () => { events.push("close"); },
  };
  const processor = {
    port: { onmessage: null, postMessage: (message) => events.push(`boundary:${message.id}`) },
    addEventListener() {},
    removeEventListener() {},
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      context,
      { disconnect() {} },
      processor,
      { disconnect() {} },
    );
    processor.port.onmessage({ data: { type: "samples", pcm: new Int16Array([700, -700]) } });
    const stopped = capture.stop(async () => "final answer");
    await Promise.resolve();
    assert.deepEqual(events.slice(0, 2), ["resume", "boundary:1"]);
    processor.port.onmessage({ data: { type: "boundary", id: 1 } });
    const captured = await stopped;
    assert.equal(await captured.transcript, "final answer");
    const wav = new DataView(await captured.blob.arrayBuffer());
    assert.equal(wav.getInt16(44, true), 700);
    assert.equal(wav.getInt16(46, true), -700);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("final stop still times out when a hidden document never resolves AudioContext resume", async () => {
  const originalWindow = globalThis.window;
  const timers = new Map();
  let nextTimer = 1;
  globalThis.window = {
    setTimeout: (callback) => {
      const id = nextTimer++;
      timers.set(id, callback);
      return id;
    },
    clearTimeout: (id) => timers.delete(id),
  };
  const sent = [];
  const failures = [];
  const context = {
    sampleRate: 16000,
    state: "suspended",
    resume: () => new Promise(() => {}),
    close: async () => {},
  };
  const processor = {
    port: { onmessage: null, postMessage: (message) => sent.push(message) },
    addEventListener() {},
    removeEventListener() {},
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      context,
      { disconnect() {} },
      processor,
      { disconnect() {} },
      (error) => failures.push(error.message),
      undefined,
      1,
      2,
    );
    const stopped = capture.stop();
    const rejection = assert.rejects(stopped, /did not finish its boundary/);
    assert.deepEqual(sent, [{ type: "boundary", id: 1 }]);
    timers.get(1)();
    await rejection;
    assert.equal(failures.length, 1);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("a permanent worklet failure rejects the pending boundary explicitly", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout };
  const listeners = new Map();
  const processor = {
    port: { onmessage: null, postMessage() {} },
    addEventListener: (type, listener) => listeners.set(type, listener),
    removeEventListener: (type) => listeners.delete(type),
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000, close: async () => {} },
      { disconnect() {} },
      processor,
      { disconnect() {} },
    );
    const pending = capture.closeTurn();
    listeners.get("processorerror")();
    await assert.rejects(pending, /Live answer capture stopped unexpectedly/);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("a boundary postMessage failure is terminal and stops capture immediately", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout };
  const failures = [];
  const processor = {
    port: {
      onmessage: null,
      postMessage() { throw new Error("audio worklet port is closed"); },
    },
    addEventListener() {},
    removeEventListener() {},
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000, close: async () => {} },
      { disconnect() {} },
      processor,
      { disconnect() {} },
      (error) => failures.push(error.message),
    );
    await assert.rejects(capture.closeTurn(), /audio worklet port is closed/);
    assert.deepEqual(failures, ["audio worklet port is closed"]);
    assert.equal(processor.port.onmessage, null);
    await assert.rejects(capture.closeTurn(), /audio worklet port is closed/);
  } finally {
    globalThis.window = originalWindow;
  }
});

test("a worklet failure without a pending boundary is reported and stays terminal", async () => {
  const originalWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout };
  const listeners = new Map();
  const failures = [];
  const processor = {
    port: { onmessage: null, postMessage() {} },
    addEventListener: (type, listener) => listeners.set(type, listener),
    removeEventListener: (type) => listeners.delete(type),
    disconnect() {},
  };
  try {
    const capture = new InterviewTurnCapture(
      { sampleRate: 16000, close: async () => {} },
      { disconnect() {} },
      processor,
      { disconnect() {} },
      (error) => failures.push(error.message),
    );
    const onProcessorError = listeners.get("processorerror");
    onProcessorError();
    onProcessorError();
    assert.deepEqual(failures, ["Live answer capture stopped unexpectedly. Please retry the recording."]);
    await assert.rejects(capture.closeTurn(), /Live answer capture stopped unexpectedly/);
    await assert.rejects(capture.stop(), /Live answer capture stopped unexpectedly/);
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
