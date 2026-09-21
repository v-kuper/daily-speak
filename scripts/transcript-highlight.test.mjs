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

