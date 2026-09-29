import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const { buildCartesiaWebSocketURL, CartesiaRealtimeTranscriber } = load("src/lib/cartesiaRealtime.ts");

const config = {
  token: "short lived/token",
  expiresAt: "2026-09-28T12:00:00Z",
  websocketUrl: "wss://api.cartesia.ai/stt/websocket?cartesia_version=2026-08-14",
  model: "ink-2",
  encoding: "pcm_s16le",
  sampleRate: 16000,
};

class FakeSocket {
  readyState = 0;
  binaryType = "";
  sent = [];
  listeners = new Map();
  throwOnFinalize = false;
  throwOnBinary = false;
  throwOnClose = false;

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  emit(type, event = {}) {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }

  open() {
    this.readyState = 1;
    this.emit("open");
  }

  message(value) {
    this.emit("message", { data: JSON.stringify(value) });
  }

  send(value) {
    if ((value === "finalize" && this.throwOnFinalize) || (value instanceof ArrayBuffer && this.throwOnBinary)
      || (value === "close" && this.throwOnClose)) {
      throw new Error("socket closed during send");
    }
    this.sent.push(value);
  }

  close(code) {
    this.readyState = 3;
    this.emit("close", { code });
  }
}

const connect = async (onTranscript, finalizeTimeoutMs = 5000, onFailure) => {
  let socket;
  let url;
  const pending = CartesiaRealtimeTranscriber.connect(config, (value) => {
    url = value;
    socket = new FakeSocket();
    return socket;
  }, onFailure, onTranscript, 5000, finalizeTimeoutMs);
  socket.open();
  return { client: await pending, socket, url };
};

test("Cartesia WebSocket URL keeps the protocol version and uses the short lived token", () => {
  const url = new URL(buildCartesiaWebSocketURL(config));
  assert.equal(url.protocol, "wss:");
  assert.equal(url.searchParams.get("cartesia_version"), "2026-08-14");
  assert.equal(url.searchParams.get("access_token"), config.token);
  assert.equal(url.searchParams.get("model"), "ink-2");
  assert.equal(url.searchParams.get("encoding"), "pcm_s16le");
  assert.equal(url.searchParams.get("sample_rate"), "16000");
});

test("final deltas emitted before and after finalize form one verbatim answer", async () => {
  const { client, socket, url } = await connect();
  assert.equal(url, buildCartesiaWebSocketURL(config));

  client.beginTurn(1);
  client.sendPCM(new Int16Array([11, 22]));
  assert.ok(socket.sent[0] instanceof ArrayBuffer);
  socket.message({ type: "transcript", is_final: true, text: "Hello " });
  socket.message({ type: "transcript", is_final: false, text: "ignored provisional" });
  socket.message({ type: "transcript", is_final: true, text: "world" });

  const answer = client.finalizeTurn();
  assert.equal(socket.sent.at(-1), "finalize");
  socket.message({ type: "transcript", is_final: true, text: "!" });
  socket.message({ type: "flush_done", request_id: "request-1" });

  assert.equal(await answer, "Hello world!");
});

test("PCM for the next answer stays buffered until the previous flush completes", async () => {
  const { client, socket } = await connect();
  client.beginTurn(1);
  const first = client.finalizeTurn();
  client.beginTurn(2);
  client.sendPCM(new Int16Array([101]));
  const second = client.finalizeTurn();
  client.beginTurn(3);
  client.sendPCM(new Int16Array([202]));

  assert.deepEqual(socket.sent, ["finalize"]);
  socket.message({ type: "transcript", is_final: true, text: "first" });
  socket.message({ type: "flush_done" });
  assert.equal(await first, "first");
  assert.ok(socket.sent[1] instanceof ArrayBuffer);
  assert.equal(socket.sent[2], "finalize");
  assert.equal(socket.sent.length, 3, "third-turn PCM is still buffered");

  socket.message({ type: "transcript", is_final: true, text: "second" });
  socket.message({ type: "flush_done" });
  assert.equal(await second, "second");
  assert.ok(socket.sent[3] instanceof ArrayBuffer);

  await client.close();
  assert.equal(socket.sent.at(-1), "close");
});

test("live snapshots concatenate final deltas verbatim and replace the interim hypothesis", async () => {
  const snapshots = [];
  const { client, socket } = await connect((snapshot) => snapshots.push(snapshot));
  client.beginTurn(4);

  socket.message({ type: "transcript", is_final: false, text: "I goed" });
  socket.message({ type: "transcript", is_final: false, text: "I went" });
  socket.message({ type: "transcript", is_final: true, text: "I went " });
  socket.message({ type: "transcript", is_final: false, text: "home" });

  assert.deepEqual(snapshots.at(-1), {
    turnSeq: 4,
    finalText: "I went ",
    interimText: "home",
    captionText: "home",
  });

  const answer = client.finalizeTurn(4);
  socket.message({ type: "transcript", is_final: true, text: "home." });
  socket.message({ type: "flush_done" });

  assert.equal(await answer, "I went home.");
  assert.deepEqual(snapshots.at(-1), {
    turnSeq: 4,
    finalText: "I went home.",
    interimText: "",
    captionText: "",
  });
});

test("rapid turn boundaries keep late final deltas attached to the matching answer", async () => {
  const snapshots = [];
  const { client, socket } = await connect((snapshot) => snapshots.push(snapshot));
  client.beginTurn(1);
  socket.message({ type: "transcript", is_final: true, text: "first " });
  const first = client.finalizeTurn(1);
  client.beginTurn(2);
  client.sendPCM(new Int16Array([101]));
  const second = client.finalizeTurn(2);
  client.beginTurn(3);

  socket.message({ type: "transcript", is_final: false, text: "late hypothesis" });
  socket.message({ type: "transcript", is_final: true, text: "answer" });
  socket.message({ type: "flush_done" });
  assert.equal(await first, "first answer");

  socket.message({ type: "transcript", is_final: true, text: "second answer" });
  socket.message({ type: "flush_done" });
  assert.equal(await second, "second answer");

  assert.deepEqual(snapshots.filter((snapshot) => snapshot.finalText || snapshot.interimText), [
    { turnSeq: 1, finalText: "first ", interimText: "", captionText: "first " },
    { turnSeq: 1, finalText: "first ", interimText: "late hypothesis", captionText: "late hypothesis" },
    { turnSeq: 1, finalText: "first answer", interimText: "", captionText: "answer" },
    { turnSeq: 1, finalText: "first answer", interimText: "", captionText: "" },
    { turnSeq: 2, finalText: "second answer", interimText: "", captionText: "second answer" },
    { turnSeq: 2, finalText: "second answer", interimText: "", captionText: "" },
  ]);
  assert.equal(snapshots.some((snapshot) => snapshot.turnSeq === 3 && (snapshot.finalText || snapshot.interimText)), false);
});

test("a missing flush rejects active and queued turns so WAV fallback can continue", async () => {
  const failures = [];
  const { client, socket } = await connect(undefined, 5, (failure) => failures.push(failure));
  client.beginTurn(1);
  const first = client.finalizeTurn(1).catch((error) => `fallback:${error.message}`);
  client.beginTurn(2);
  client.sendPCM(new Int16Array([101]));
  const second = client.finalizeTurn(2).catch((error) => `fallback:${error.message}`);

  assert.equal(await first, "fallback:Realtime transcription did not finish the answer.");
  assert.equal(await second, "fallback:Realtime transcription did not finish the answer.");
  assert.equal(socket.readyState, 3);
  assert.deepEqual(failures.map((failure) => failure.reason), ["finalize_timeout"]);
});

test("an unexpected close reports a safe diagnostic reason and keeps the answer available for fallback", async () => {
  const failures = [];
  const { client, socket } = await connect(undefined, 5000, (failure) => failures.push(failure));
  client.beginTurn(1);
  socket.close(1006);

  await assert.rejects(client.finalizeTurn(1), /closed unexpectedly/);
  assert.deepEqual(failures.map((failure) => [failure.reason, failure.closeCode]), [["unexpected_close", 1006]]);
});

test("a failed finalize send rejects the answer instead of leaving live transcription pending", async () => {
  const failures = [];
  const { client, socket } = await connect(undefined, 5000, (failure) => failures.push(failure));
  client.beginTurn(1);
  socket.throwOnFinalize = true;

  const answer = client.finalizeTurn(1);
  await assert.rejects(answer, /finalize could not be sent/);
  assert.deepEqual(failures.map((failure) => failure.reason), ["transport_error"]);
});

test("a failed buffered audio send rejects the queued answer", async () => {
  const failures = [];
  const { client, socket } = await connect(undefined, 5000, (failure) => failures.push(failure));
  client.beginTurn(1);
  const first = client.finalizeTurn(1);
  client.beginTurn(2);
  client.sendPCM(new Int16Array([101]));
  const second = client.finalizeTurn(2);
  socket.throwOnBinary = true;
  socket.message({ type: "flush_done" });

  assert.equal(await first, "");
  await assert.rejects(second, /audio could not be sent/);
  assert.deepEqual(failures.map((failure) => failure.reason), ["audio_send_failed"]);
});

test("a failed graceful close does not leave an unhandled socket error", async () => {
  const failures = [];
  const { client, socket } = await connect(undefined, 5000, (failure) => failures.push(failure));
  socket.throwOnClose = true;

  await client.close();
  assert.equal(socket.readyState, 3);
  assert.deepEqual(failures.map((failure) => failure.reason), ["transport_error"]);
});
