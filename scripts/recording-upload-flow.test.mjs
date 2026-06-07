import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const speakScreen = readFileSync("src/components/SpeakScreen.tsx", "utf8");
const appSlice = readFileSync("src/store/slices/appSlice.ts", "utf8");

test("SpeakScreen uploads a final complete Blob to the recording session", () => {
  assert.match(speakScreen, /uploadRecordingFinalAudio/);
  assert.match(speakScreen, /\/api\/recording-sessions\/\$\{encodeURIComponent\(sessionId\)\}\/audio/);
});

test("SpeakScreen falls back to full save if final session audio upload fails", () => {
  assert.match(speakScreen, /setRecordingUploadSessionId\(null\)/);
  assert.match(speakScreen, /Full recording will be uploaded when you save/);
});

test("SpeakScreen prepares local playback without waiting for final upload", () => {
  assert.doesNotMatch(speakScreen, /Promise\.all\(\[readBlobAsDataUrl\(blob\), finalUpload\]\)/);
  assert.match(speakScreen, /void readBlobAsDataUrl\(blob\)/);
  assert.match(speakScreen, /finalAudioUploadPromiseRef/);
});

test("Redux supports optimistic background recording save", () => {
  assert.match(appSlice, /showBackgroundRecordingSave/);
  assert.match(appSlice, /backgroundSaveRecordingId/);
  assert.match(appSlice, /status: "processing"/);
});
