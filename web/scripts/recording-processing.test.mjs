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

test("failed stages have block-specific retry labels", () => {
  assert.equal(processing.recordingRetryLabel("transcribing"), "Retry transcription");
  assert.equal(processing.recordingRetryLabel("suggestions"), "Retry AI analysis");
  assert.equal(processing.recordingRetryLabel("rewriting"), "Retry natural version");
  assert.equal(processing.recordingRetryLabel(null), null);
});

test("shadowing progress is hidden until a natural version is ready", () => {
  assert.equal(processing.shouldShowShadowingProgress({
    recordingStatus: "failed",
    correctedTranscript: "",
    shadowingStatus: "pending",
  }), false);
  assert.equal(processing.shouldShowShadowingProgress({
    recordingStatus: "ready",
    correctedTranscript: "I went home.",
    shadowingStatus: "pending",
  }), true);
  assert.equal(processing.shouldShowShadowingProgress({
    recordingStatus: "ready",
    correctedTranscript: "I went home.",
    shadowingStatus: "ready",
  }), false);
});

test("details offers retry in the block that owns the failed stage", () => {
  const detailsSource = readFileSync("src/components/DetailsScreen.tsx", "utf8");

  assert.match(detailsSource, /dispatch\(retryRecordingProcessing\(recording\.id\)\)/);
  assert.match(detailsSource, /renderProcessingRetry\("transcribing"\)/);
  assert.match(detailsSource, /renderProcessingRetry\("suggestions"\)/);
  assert.match(detailsSource, /renderProcessingRetry\("rewriting"\)/);
});
