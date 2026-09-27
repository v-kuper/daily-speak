import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const speakScreen = readFileSync("src/components/SpeakScreen.tsx", "utf8");
const appSlice = readFileSync("src/store/slices/appSlice.ts", "utf8");
const mediaUpload = readFileSync("src/lib/mediaUpload.ts", "utf8");

test("authenticated recordings use the shared v1 multipart media contract", () => {
  assert.match(appSlice, /uploadMedia/);
  assert.match(appSlice, /\/api\/v1\/recordings/);
  assert.match(mediaUpload, /\/api\/v1\/media\/uploads/);
});

test("legacy live recording sessions are absent from the web flow", () => {
  assert.doesNotMatch(speakScreen + appSlice, /recording-sessions|recordingUploadSessionId/);
});

test("SpeakScreen prepares local playback before the explicit save", () => {
  assert.match(speakScreen, /void readBlobAsDataUrl\(blob\)/);
  assert.doesNotMatch(speakScreen, /finalAudioUploadPromiseRef/);
});

test("Redux supports optimistic background recording save", () => {
  assert.match(appSlice, /showBackgroundRecordingSave/);
  assert.match(appSlice, /backgroundSaveRecordingId/);
  assert.match(appSlice, /status: "processing"/);
});
