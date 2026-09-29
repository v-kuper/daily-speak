import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import ts from "typescript";

async function importTypeScriptModule(path) {
  const source = readFileSync(path, "utf8");
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2022 },
  });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);
}

const history = await importTypeScriptModule("src/lib/dailyQuestionHistory.ts");

const recording = (overrides) => ({
  practiceType: "topic",
  topic: "What place do you enjoy visiting?",
  transcript: "I enjoy the park.",
  interviewTurns: [],
  ...overrides,
});

test("daily question history includes only questions that received answers", () => {
  const questions = history.collectRecentAnsweredQuestions([
    recording({
      interviewTurns: [
        { question: "What place do you enjoy visiting?", answerText: "The park." },
        { question: "Why do you like it?", answerText: "It is quiet." },
        { question: "Who goes with you?", answerText: "" },
      ],
    }),
    recording({ practiceType: "free_talk", topic: "Free talk", transcript: "Hello." }),
  ]);
  assert.deepEqual(questions, ["What place do you enjoy visiting?", "Why do you like it?"]);
});

test("daily question history preserves recent order, removes duplicates, and respects its limit", () => {
  const questions = history.collectRecentAnsweredQuestions([
    recording({ topic: "Question one?" }),
    recording({ topic: " question ONE? " }),
    recording({ topic: "Question two?" }),
  ], 2);
  assert.deepEqual(questions, ["Question one?", "Question two?"]);
  assert.notEqual(history.questionHistoryKey(questions), "");
});
