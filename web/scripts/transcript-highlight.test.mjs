import assert from "node:assert/strict";
import test from "node:test";
import { createTypeScriptLoader } from "./helpers/load-typescript.mjs";

const highlight = createTypeScriptLoader()("src/lib/transcriptHighlight.ts");

test("new feedback with a lost anchor does not guess a legacy location", () => {
  const segments = highlight.buildTranscriptSegments("I go home.", [{ id: "correction-1", wrong: "go" }]);
  assert.equal(segments.some(segment => segment.isError), false);
});

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

test("ambiguous legacy repetitions are kept unmarked", () => {
  const segments = highlight.buildTranscriptSegments("I go, then I go.", [{ wrong: "I go", severity: "medium" }]);
  assert.deepEqual(
    segments.filter((part) => part.isError).map((part) => part.text),
    [],
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

test("anchored correction marks only the wrong context, using UTF-16 offsets", () => {
  const text = "😀 I go every day. Yesterday I go.";
  const segments = highlight.buildTranscriptSegments(text, [{ wrong: "I go", severity: "medium", span: { start: 29, end: 33 } }]);
  assert.deepEqual(segments.filter(part => part.isError).map(part => part.text), ["I go"]);
  assert.equal(segments[0].text, "😀 I go every day. Yesterday ");
});

test("exact case and word boundaries keep cards independently reachable", () => {
  const segments = highlight.buildTranscriptSegments("Борщ was good. I ate борщ. She is going.", [
    { wrong: "Борщ", severity: "medium" }, { wrong: "борщ", severity: "medium" }, { wrong: "go", severity: "medium" },
  ]);
  assert.deepEqual(segments.filter(part => part.isError).map(part => [part.text, part.feedbackIndex]), [["Борщ", 0], ["борщ", 1]]);
});

test("invalid anchors never fall back to a different occurrence", () => {
  const segments = highlight.buildTranscriptSegments("I go home.", [{ wrong: "I go", severity: "major", span: { start: 2, end: 6 } }]);
  assert.equal(segments.some(part => part.isError), false);
});

test("interview anchors remain inside their answer, including skipped sequences", () => {
  const turns = [{ sequence: 1, question: "Usually?", answerText: "I go home." }, { sequence: 4, question: "Yesterday?", answerText: "I go home." }];
  const result = highlight.buildConversationTranscriptTurns(turns, [{ wrong: "I go", severity: "medium", span: { start: 0, end: 4, turnSequence: 4 } }]);
  assert.equal(result[0].answerSegments.some(part => part.isError), false);
  assert.equal(result[1].answerSegments.some(part => part.isError), true);
  const legacy = highlight.buildConversationTranscriptTurns(turns, [{ wrong: "I go", severity: "medium" }]);
  assert.equal(legacy.some(turn => turn.answerSegments.some(part => part.isError)), false);
});
