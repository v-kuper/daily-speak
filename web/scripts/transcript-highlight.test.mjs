import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import ts from "typescript";

async function importTypeScriptModule(path) {
  const source = readFileSync(path, "utf8");
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.ES2022,
      target: ts.ScriptTarget.ES2022,
    },
  });

  const moduleUrl = `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`;
  return import(moduleUrl);
}

const highlight = await importTypeScriptModule("src/lib/transcriptHighlight.ts");

test("highest severity wins only on overlapping characters", () => {
  const segments = highlight.buildTranscriptSegments("I am forgot this.", [
    { wrong: "am forgot", severity: "minor" },
    { wrong: "forgot", severity: "major" },
  ]);

  assert.deepEqual(
    segments.filter((part) => part.isError).map((part) => [part.text, part.severity]),
    [
      ["am ", "minor"],
      ["forgot", "major"],
    ],
  );
});

test("every repeated exact phrase is highlighted", () => {
  const segments = highlight.buildTranscriptSegments("I go, then I go.", [{ wrong: "I go", severity: "medium" }]);
  assert.deepEqual(
    segments.filter((part) => part.isError).map((part) => part.text),
    ["I go", "I go"],
  );
});

test("unknown or missing severity keeps the legacy neutral error style", () => {
  const segments = highlight.buildTranscriptSegments("wrong and odd", [
    { wrong: "wrong" },
    { wrong: "odd", severity: "critical" },
  ]);
  assert.deepEqual(
    segments.filter((part) => part.isError).map((part) => part.severity),
    [null, null],
  );
});

test("twenty-five different suggestions remain highlightable", () => {
  const suggestions = Array.from({ length: 25 }, (_, index) => ({ wrong: `e${index}`, severity: "minor" }));
  const transcript = suggestions.map((item) => item.wrong).join(" ");
  const segments = highlight.buildTranscriptSegments(transcript, suggestions);
  assert.equal(segments.filter((part) => part.isError).length, 25);
});

test("conversation transcript highlights learner answers without marking interviewer questions", () => {
  const [turn] = highlight.buildConversationTranscriptTurns([{
    sequence: 1,
    question: "Did you say I goed home?",
    askedAtMs: 0,
    endedAtMs: 3000,
    answerText: "I goed home yesterday.",
    correctedAnswerText: "I went home yesterday.",
    answerSource: "final",
    answerAlignment: null,
  }], [{ wrong: "I goed home", severity: "major" }]);

  assert.equal(turn.question, "Did you say I goed home?");
  assert.equal(turn.hasAnswer, true);
  assert.deepEqual(
    turn.answerSegments.filter((part) => part.isError).map((part) => part.text),
    ["I goed home"],
  );
});

test("corrected conversation keeps each question and renders the natural answer without error marks", () => {
  const [turn] = highlight.buildConversationTranscriptTurns([{
    sequence: 1,
    question: "Where did you go?",
    askedAtMs: 0,
    endedAtMs: 3000,
    answerText: "I goed home.",
    correctedAnswerText: "I went home.",
    answerSource: "final",
    answerAlignment: null,
  }], [{ wrong: "I went home", severity: "major" }], "corrected");

  assert.equal(turn.question, "Where did you go?");
  assert.equal(turn.hasAnswer, true);
  assert.deepEqual(turn.answerSegments, [{ text: "I went home.", isError: false, severity: null }]);
});

test("strengths link to their card index and corrections win on overlap", () => {
  const segments = highlight.buildTranscriptSegments(
    "I have lived here for five years.",
    [{ wrong: "lived", severity: "medium" }],
    [{ excerpt: "I have lived here for five years" }],
  );
  const correction = segments.find((part) => part.isError);
  const strengths = segments.filter((part) => part.isStrength);
  assert.equal(correction.feedbackIndex, 0);
  assert.equal(strengths.some((part) => part.text.includes("lived")), false);
  assert.equal(strengths.every((part) => part.feedbackIndex === 0), true);
});
