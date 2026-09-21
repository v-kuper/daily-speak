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

const suggestions = await importTypeScriptModule("src/lib/suggestions.ts");

test("all valid AI suggestions reach transcript highlighting and the review list", () => {
  const input = Array.from({ length: 25 }, (_, index) => ({
    wrong: `wrong ${index}`,
    right: `right ${index}`,
    explanation: `explanation ${index}`,
  }));

  const parsed = suggestions.parseSuggestions(input);

  assert.equal(parsed.length, 25);
  assert.deepEqual(parsed.at(-1), input.at(-1));
});

test("invalid suggestions are removed without limiting valid ones", () => {
  const parsed = suggestions.parseSuggestions([
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
    { wrong: "missing fields" },
    null,
  ]);

  assert.deepEqual(parsed, [
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
  ]);
});

test("valid metadata is kept and old suggestions stay metadata-free", () => {
  const [modern, legacy] = suggestions.parseSuggestions([
    {
      wrong: "she go",
      right: "she goes",
      explanation: "Use agreement.",
      category: "verb_grammar",
      severity: "medium",
      ruleId: "subject-verb-agreement",
      learningReference: {
        id: "subject-verb-agreement",
        title: "Subject-verb agreement",
        summary: "Match subject and verb.",
        url: "https://dictionary.cambridge.org/us/grammar/british-grammar/subject-verb-agreement",
      },
    },
    { wrong: "I go", right: "I went", explanation: "Use past tense." },
  ]);

  assert.equal(modern.severity, "medium");
  assert.equal(modern.learningReference.id, "subject-verb-agreement");
  assert.equal(legacy.severity, undefined);
  assert.equal(legacy.learningReference, undefined);
});

test("unsafe reference URL is dropped without dropping the correction", () => {
  const [parsed] = suggestions.parseSuggestions([
    {
      wrong: "she go",
      right: "she goes",
      explanation: "Use agreement.",
      category: "verb_grammar",
      severity: "medium",
      learningReference: { id: "x", title: "X", summary: "X", url: "javascript:alert(1)" },
    },
  ]);

  assert.equal(parsed.wrong, "she go");
  assert.equal(parsed.learningReference, undefined);
});
