import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const audioDrafts = load("src/lib/recordingDraftAudio.ts");

test("recorded audio stays behind an opaque key and can be removed after save", async () => {
  const source = new Blob(["recorded audio"], { type: "audio/webm" });
  const key = await audioDrafts.storeRecordingDraftAudio(source);

  assert.match(key, /^recording-audio:/);
  assert.equal(audioDrafts.isRecordingDraftAudioKey(key), true);
  assert.equal(await (await audioDrafts.loadRecordingDraftAudio(key)).text(), "recorded audio");

  await audioDrafts.deleteRecordingDraftAudio(key);
  await assert.rejects(
    () => audioDrafts.loadRecordingDraftAudio(key),
    /no longer available/i,
  );
});

test("recorded audio storage rejects empty and non-audio blobs", async () => {
  await assert.rejects(
    () => audioDrafts.storeRecordingDraftAudio(new Blob([], { type: "audio/webm" })),
    /invalid/i,
  );
  await assert.rejects(
    () => audioDrafts.storeRecordingDraftAudio(new Blob(["text"], { type: "text/plain" })),
    /invalid/i,
  );
});
