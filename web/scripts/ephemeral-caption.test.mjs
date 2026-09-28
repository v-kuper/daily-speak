import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const {
  DEFAULT_LIVE_CAPTION_IDLE_MS,
  EphemeralCaptionController,
} = load("src/lib/ephemeralCaption.ts");

class FakeScheduler {
  nextHandle = 1;
  pending = new Map();
  cancelled = [];

  schedule(callback, delayMs) {
    const handle = this.nextHandle++;
    this.pending.set(handle, { callback, delayMs });
    return handle;
  }

  cancel(handle) {
    this.cancelled.push(handle);
    this.pending.delete(handle);
  }

  fire(handle) {
    const scheduled = this.pending.get(handle);
    if (!scheduled) return;
    this.pending.delete(handle);
    scheduled.callback();
  }
}

test("the latest phrase replaces the previous subtitle and disappears after inactivity", () => {
  const published = [];
  const scheduler = new FakeScheduler();
  const captions = new EphemeralCaptionController((caption) => published.push(caption), scheduler);

  captions.beginTurn(1);
  captions.update({ turnSeq: 1, finalText: "", interimText: "first phrase", captionText: "first phrase" });
  assert.deepEqual(published, [null, "first phrase"]);
  assert.equal(scheduler.pending.get(1)?.delayMs, DEFAULT_LIVE_CAPTION_IDLE_MS);

  captions.update({ turnSeq: 1, finalText: "", interimText: "second phrase", captionText: "second phrase" });
  assert.deepEqual(scheduler.cancelled, [1]);
  assert.deepEqual(published, [null, "first phrase", "second phrase"]);
  assert.equal(scheduler.pending.has(1), false);
  assert.equal(scheduler.pending.get(2)?.delayMs, DEFAULT_LIVE_CAPTION_IDLE_MS);

  scheduler.fire(2);
  assert.deepEqual(published, [null, "first phrase", "second phrase", null]);
});

test("stable deltas form one phrase, then a new phrase starts after silence", () => {
  const published = [];
  const scheduler = new FakeScheduler();
  const captions = new EphemeralCaptionController((caption) => published.push(caption), scheduler, 25);

  captions.beginTurn(3);
  captions.update({ turnSeq: 3, finalText: "I went ", interimText: "", captionText: "I went " });
  captions.update({ turnSeq: 3, finalText: "I went ", interimText: "home", captionText: "home" });
  assert.deepEqual(published.slice(-2), ["I went", "I went home"]);
  captions.update({ turnSeq: 3, finalText: "I went home. ", interimText: "", captionText: "home. " });
  scheduler.fire(3);

  captions.update({ turnSeq: 3, finalText: "I went home. ", interimText: "Today", captionText: "Today" });
  assert.equal(published.at(-1), "Today");
});

test("turn changes, clear, and dispose reject stale subtitle updates", () => {
  const published = [];
  const scheduler = new FakeScheduler();
  const captions = new EphemeralCaptionController((caption) => published.push(caption), scheduler, 25);

  captions.beginTurn(1);
  captions.update({ turnSeq: 1, finalText: "visible", interimText: "", captionText: "visible" });
  captions.beginTurn(2);
  captions.update({ turnSeq: 1, finalText: "late", interimText: "", captionText: "late" });
  assert.equal(published.at(-1), null);

  captions.update({ turnSeq: 2, finalText: "new turn", interimText: "", captionText: "new turn" });
  captions.clear();
  assert.equal(published.at(-1), null);
  captions.dispose();
  captions.beginTurn(3);
  captions.update({ turnSeq: 3, finalText: "ignored", interimText: "", captionText: "ignored" });
  assert.equal(published.at(-1), null);
  assert.deepEqual(scheduler.cancelled, [1, 2]);
});
