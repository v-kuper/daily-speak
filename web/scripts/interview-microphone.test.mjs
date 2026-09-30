import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const { InterviewMicrophone } = createTypeScriptLoader()("src/lib/interviewMicrophone.ts");
const stream = () => {
  const tracks = [0, 1].map(() => ({ enabled: true, stop() { throw new Error("Muting must not stop the recording."); } }));
  return { tracks, getAudioTracks: () => tracks };
};

test("manual mute survives question completion, cancellation and the next question", () => {
  const microphone = new InterviewMicrophone();
  const input = stream();
  microphone.attach(input);
  assert.equal(microphone.toggleUserMuted(), true);
  for (const speaking of [true, false, false, true, false]) {
    microphone.setQuestionSpeechMuted(speaking);
    assert.ok(input.tracks.every(track => !track.enabled));
  }
  assert.equal(microphone.toggleUserMuted(), false);
  assert.ok(input.tracks.every(track => track.enabled));
});

test("unmuting during question playback waits until speech finishes", () => {
  const microphone = new InterviewMicrophone();
  const input = stream();
  microphone.attach(input);
  microphone.setQuestionSpeechMuted(true);
  microphone.toggleUserMuted();
  microphone.toggleUserMuted();
  assert.ok(input.tracks.every(track => !track.enabled));
  microphone.setQuestionSpeechMuted(false);
  assert.ok(input.tracks.every(track => track.enabled));
});

test("a fresh recording resets mute without enabling the released stream", () => {
  const microphone = new InterviewMicrophone();
  const first = stream();
  microphone.attach(first);
  microphone.toggleUserMuted();
  microphone.attach(null);
  microphone.reset();
  const second = stream();
  microphone.attach(second);
  assert.ok(first.tracks.every(track => !track.enabled));
  assert.ok(second.tracks.every(track => track.enabled));
});
