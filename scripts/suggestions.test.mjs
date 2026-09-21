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
