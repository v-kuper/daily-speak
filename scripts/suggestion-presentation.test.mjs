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

const presentation = await importTypeScriptModule("src/lib/suggestionPresentation.ts");

test("category and severity labels are explicit text", () => {
  assert.equal(presentation.suggestionCategoryLabel("language_switch"), "Russian → English");
  assert.equal(presentation.suggestionCategoryLabel("sentence_structure"), "Sentence structure");
  assert.equal(presentation.suggestionSeverityLabel("major"), "Major");
  assert.equal(presentation.suggestionSeverityClass("minor"), "suggestion-severity-minor");
});

test("missing or unknown legacy metadata produces no badge labels", () => {
  assert.equal(presentation.suggestionCategoryLabel(undefined), null);
  assert.equal(presentation.suggestionCategoryLabel("invented"), null);
  assert.equal(presentation.suggestionSeverityLabel(undefined), null);
  assert.equal(presentation.suggestionSeverityLabel("critical"), null);
  assert.equal(presentation.suggestionSeverityClass(undefined), null);
});
