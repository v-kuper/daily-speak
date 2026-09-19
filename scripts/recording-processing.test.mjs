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

const processing = await importTypeScriptModule("src/lib/recordingProcessing.ts");

test("processing stages have user-facing progress labels", () => {
  assert.equal(processing.recordingProcessingLabel("transcribing"), "Transcribing audio...");
  assert.equal(processing.recordingProcessingLabel("suggestions"), "Analyzing your English...");
  assert.equal(processing.recordingProcessingLabel("rewriting"), "Creating a natural version...");
});

test("missing processing stage keeps a useful generic label", () => {
  assert.equal(processing.recordingProcessingLabel(null), "Processing recording...");
});

test("only supported server processing stages are accepted", () => {
  assert.equal(processing.parseRecordingProcessingStage("rewriting"), "rewriting");
  assert.equal(processing.parseRecordingProcessingStage("unexpected"), null);
  assert.equal(processing.parseRecordingProcessingStage(undefined), null);
});
