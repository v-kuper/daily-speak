import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const speakScreen = readFileSync("src/components/SpeakScreen.tsx", "utf8");

test("SpeakScreen uploads a final complete Blob to the recording session", () => {
  assert.match(speakScreen, /uploadRecordingFinalAudio/);
  assert.match(speakScreen, /\/api\/recording-sessions\/\$\{encodeURIComponent\(sessionId\)\}\/audio/);
});

test("SpeakScreen falls back to full save if final session audio upload fails", () => {
  assert.match(speakScreen, /setRecordingUploadSessionId\(null\)/);
  assert.match(speakScreen, /Full recording will be uploaded when you save/);
});
