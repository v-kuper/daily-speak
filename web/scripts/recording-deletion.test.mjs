import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const deletion = load("src/lib/recordingDeletion.ts");
const app = load("src/store/slices/appSlice.ts");

const firstRecording = {
  id: "recording-1",
  topic: "Morning routine",
  duration: 30,
  timestamp: "2026-09-21T09:00:00Z",
  status: "ready",
  transcript: "I wake up early.",
  correctedTranscript: "I wake up early.",
  suggestions: [],
  processingStage: null,
  practiceType: "topic",
  audioDataUrl: "https://api.example.test/uploads/recordings/recording-1.webm",
  photoDataUrl: null,
  photoObject: null,
  processingError: null,
  shadowingStatus: "ready",
  shadowingAudioUrl: "https://api.example.test/uploads/recordings/recording-1-shadowing.wav",
  shadowingError: null,
  shadowingUpdatedAt: "2026-09-21T09:01:00Z",
};
const secondRecording = { ...firstRecording, id: "recording-2" };

const recordingState = () => ({
  ...app.default(undefined, { type: "test/initialize" }),
  isAuthenticated: true,
  currentScreen: "details",
  activeTab: "history",
  currentRecordingId: "recording-1",
  recordings: [firstRecording, secondRecording],
  isPlaying: true,
  playbackPosition: 12,
  recordingRetryStatuses: { "recording-1": "loading", "recording-2": "loading" },
  recordingRetryErrors: { "recording-1": "Retry failed" },
});

test("recording deletion needs no publication or sharing state", () => {
  const state = app.default(undefined, { type: "test/initialize" });
  assert.deepEqual(Object.keys(state).filter((key) => /feed|share|copyMessage/i.test(key)), []);
});

test("successful deletion returns to history, resets playback, and ignores late recording responses", () => {
  let state = app.default(recordingState(), app.deleteRecording.fulfilled(
    { recordingId: "recording-1", quota: null }, "delete-request", "recording-1",
  ));
  assert.deepEqual(state.recordings, [secondRecording]);
  assert.deepEqual(state.deletedRecordingIds, ["recording-1"]);
  assert.equal(state.currentRecordingId, null);
  assert.equal(state.currentScreen, "history");
  assert.equal(state.activeTab, "history");
  assert.equal(state.isPlaying, false);
  assert.equal(state.playbackPosition, 0);
  assert.deepEqual(state.recordingRetryStatuses, { "recording-2": "loading" });
  assert.deepEqual(state.recordingRetryErrors, {});
  state = app.default(state, app.fetchRecording.fulfilled(firstRecording, "late-request", "recording-1"));
  assert.deepEqual(state.recordings, [secondRecording]);
});

test("failed deletion preserves the recording and exposes the API error", () => {
  const state = app.default(recordingState(), app.deleteRecording.rejected(
    null, "delete-request", "recording-1", "Unable to delete recording",
  ));
  assert.deepEqual(state.recordings, [firstRecording, secondRecording]);
  assert.equal(state.currentScreen, "details");
  assert.equal(state.currentRecordingId, "recording-1");
  assert.equal(state.recordingDeleteStatus, "idle");
  assert.equal(state.recordingDeleteError, "Unable to delete recording");
  assert.deepEqual(state.deletedRecordingIds, []);
});

test("removing a recording keeps unrelated recordings", () => {
  assert.equal(typeof deletion.removeRecording, "function");
  const result = deletion.removeRecording(
    [{ id: "recording-1" }, { id: "recording-2" }],
    "recording-1",
  );

  assert.deepEqual(result, [{ id: "recording-2" }]);
});

test("late responses cannot restore deleted recordings", () => {
  assert.deepEqual(
    deletion.filterDeletedRecordings([{ id: "deleted" }, { id: "kept" }], ["deleted"]),
    [{ id: "kept" }],
  );
});
