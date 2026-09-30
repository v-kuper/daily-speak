import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";
const { parseGuestPreviewCorrections, guestPreviewSegments } = createTypeScriptLoader()("src/lib/guestPreviewFeedback.ts");
const correction = { wrong: "go", right: "went", explanation: "Use the past form." };

test("the limited guest contract keeps two valid corrections and ignores account metadata", () => {
  assert.deepEqual(parseGuestPreviewCorrections([null, {}, { ...correction, category: "verb_grammar", severity: "major" }, correction, correction]), [correction, correction]);
  assert.deepEqual(parseGuestPreviewCorrections(undefined), []);
});

test("guest highlights preserve Unicode and avoid repeated or partial word matches", () => {
  const text = "😀 Yesterday I go home.";
  const segments = guestPreviewSegments(text, [correction]);
  assert.equal(segments.map(item => item.text).join(""), text);
  assert.deepEqual(segments.filter(item => item.isCorrection).map(item => item.text), ["go"]);
  for (const text of ["I go and go.", "I forgot.", "I GO."]) assert.equal(guestPreviewSegments(text, [correction]).some(item => item.isCorrection), false);
  assert.equal(guestPreviewSegments("I go.", [correction], ["I go.", "We go."]).some(item => item.isCorrection), false);
});
