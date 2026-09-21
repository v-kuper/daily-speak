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

const interview = await importTypeScriptModule("src/lib/interviewGuidance.ts");

test("selected topic stays first and follow-up order is preserved", () => {
  assert.deepEqual(
    interview.buildInterviewQuestions("  How do you learn English?  ", [
      "What helps you practise?",
      "how do you learn english?",
      " ",
      "What is still difficult?",
      "What helps you practise?",
    ]),
    ["How do you learn English?", "What helps you practise?", "What is still difficult?"],
  );
});

test("question navigation stays inside interview boundaries", () => {
  assert.equal(interview.moveInterviewQuestion(0, -1, 18), 0);
  assert.equal(interview.moveInterviewQuestion(0, 1, 18), 1);
  assert.equal(interview.moveInterviewQuestion(17, 1, 18), 17);
  assert.equal(interview.moveInterviewQuestion(4, -1, 18), 3);
  assert.equal(interview.moveInterviewQuestion(4, 1, 0), 0);
});

test("ticker offsets wrap in both directions", () => {
  assert.equal(interview.normalizeTickerOffset(105, 100), 5);
  assert.equal(interview.normalizeTickerOffset(-5, 100), 95);
  assert.equal(interview.normalizeTickerOffset(30, 0), 0);
});

test("only the active guidance request may update an interview", () => {
  assert.equal(interview.isCurrentInterviewGuidanceRequest("request-b", "request-b"), true);
  assert.equal(interview.isCurrentInterviewGuidanceRequest("request-b", "request-a"), false);
  assert.equal(interview.isCurrentInterviewGuidanceRequest(null, "request-a"), false);
});

test("guidance request identity is stable for equivalent interests", () => {
  assert.equal(
    interview.buildInterviewGuidanceRequestKey(" Topic ", ["travel", "music"], "b1"),
    interview.buildInterviewGuidanceRequestKey("Topic", ["music", "travel"], "b1"),
  );
  assert.notEqual(
    interview.buildInterviewGuidanceRequestKey("Topic A", ["travel"], "b1"),
    interview.buildInterviewGuidanceRequestKey("Topic B", ["travel"], "b1"),
  );
});
