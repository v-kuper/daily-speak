import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const load = createTypeScriptLoader();
const { interviewPracticeTurns, interviewPracticeNavigation } = load("src/lib/interviewPractice.ts");
const turn = (sequence, answerText = "My answer.") => ({ sequence, question: `Question ${sequence}?`, answerText });

test("practice follows interview order, skips unanswered turns, and preserves server identities", () => {
  const original = [turn(7), turn(2, "  "), turn(3), turn(1)];
  const turns = interviewPracticeTurns(original);
  assert.deepEqual(turns.map(item => item.sequence), [1, 3, 7]);
  assert.deepEqual(original.map(item => item.sequence), [7, 2, 3, 1]);
  const first = interviewPracticeNavigation(turns, 1);
  assert.equal(first.position, 1);
  assert.equal(first.previous, null);
  assert.equal(first.next, 3);
  const middle = interviewPracticeNavigation(turns, first.next);
  assert.equal(middle.turn, original[2]);
  assert.equal(middle.position, 2);
  assert.equal(middle.total, 3);
  assert.equal(middle.previous, 1);
  assert.equal(middle.next, 7);
  const last = interviewPracticeNavigation(turns, middle.next);
  assert.equal(last.position, 3);
  assert.equal(last.previous, 3);
  assert.equal(last.next, null);
});

test("empty, unanswered, and stale selections never open an unrelated question", () => {
  assert.deepEqual(interviewPracticeTurns(), []);
  assert.equal(interviewPracticeNavigation([], null), null);
  assert.equal(interviewPracticeNavigation([turn(4)], 1), null);
  const turns = interviewPracticeTurns([turn(1, ""), turn(4)]);
  assert.equal(interviewPracticeNavigation(turns, 1), null);
  const only = interviewPracticeNavigation(turns, 4);
  assert.equal(only.position, 1);
  assert.equal(only.total, 1);
  assert.equal(only.previous, null);
  assert.equal(only.next, null);
});
