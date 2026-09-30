import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const { recordingFeedbackSummary, recordingTitle, recordingHistoryStatus, recordingCountLabel } = createTypeScriptLoader()("src/lib/recordingPresentation.ts");
const recording = { status: "ready", topic: "Work", interviewTurns: [] };

test("history counts genuine errors independently of praise and naturalness, and waits for a completed analysis", () => {
  const analyzed = { ...recording, focusedFeedback: { answers: [
    { items: [{ kind: "blocker" }, { kind: "praise" }, { kind: "native_tip" }] },
    { items: [{ kind: "blocker" }, { kind: "blocker" }] },
  ] } };
  assert.equal(recordingFeedbackSummary(analyzed), "3 ошибки");
  assert.equal(recordingFeedbackSummary({ ...analyzed, status: "processing" }), null);
  assert.equal(recordingFeedbackSummary({ ...analyzed, status: "failed" }), null);
  assert.equal(recordingFeedbackSummary(recording), null);
  assert.equal(recordingFeedbackSummary({ ...recording, focusedFeedback: { answers: [{ items: [{ kind: "praise" }] }] } }), "Без ошибок");
});

test("history titles use the actual opening question and status distinguishes failed and partial processing", () => {
  assert.equal(recordingTitle({ ...recording, interviewTurns: [{ question: "What did you work on?" }] }), "What did you work on?");
  assert.equal(recordingTitle(recording), "Work");
  assert.equal(recordingHistoryStatus(recording), "Разбор готов");
  assert.equal(recordingHistoryStatus({ ...recording, status: "failed" }), "Разбор не завершён");
  assert.equal(recordingHistoryStatus({ ...recording, status: "processing", processingStage: "transcribing" }), "Распознаём речь");
});

test("recording counts use readable Russian plurals", () => {
  for (const [count, expected] of [[0, "0 записей"], [1, "1 запись"], [4, "4 записи"], [11, "11 записей"], [21, "21 запись"], [24, "24 записи"], [111, "111 записей"]]) {
    assert.equal(recordingCountLabel(count), expected);
  }
});
