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

const shadowing = await importTypeScriptModule("src/lib/shadowing.ts");

test("only known shadowing statuses are accepted", () => {
  for (const status of ["pending", "processing", "ready", "failed"]) {
    assert.equal(shadowing.parseShadowingStatus(status), status);
  }

  assert.equal(shadowing.parseShadowingStatus("unexpected"), "pending");
  assert.equal(shadowing.parseShadowingStatus(undefined), "pending");
});

test("shadowing schedules only when the corrected recording is ready", () => {
  const ready = {
    recordingStatus: "ready",
    correctedTranscript: "A natural sentence.",
    shadowingStatus: "pending",
    requestLoading: false,
  };

  assert.equal(shadowing.shouldScheduleShadowing(ready), true);
  assert.equal(shadowing.shouldScheduleShadowing({ ...ready, recordingStatus: "processing" }), false);
  assert.equal(shadowing.shouldScheduleShadowing({ ...ready, correctedTranscript: "   " }), false);
  assert.equal(shadowing.shouldScheduleShadowing({ ...ready, shadowingStatus: "processing" }), false);
  assert.equal(shadowing.shouldScheduleShadowing({ ...ready, requestLoading: true }), false);
});

test("recording polling covers main and shadowing processing", () => {
  assert.equal(shadowing.shouldPollRecording("processing", "pending"), true);
  assert.equal(shadowing.shouldPollRecording("ready", "processing"), true);
  assert.equal(shadowing.shouldPollRecording("ready", "ready"), false);
});

test("shadowing processing becomes stale after five minutes", () => {
  const now = Date.parse("2026-09-20T12:00:00.000Z");

  assert.equal(
    shadowing.isShadowingStale("processing", "2026-09-20T11:56:00.000Z", now),
    false,
  );
  assert.equal(
    shadowing.isShadowingStale("processing", "2026-09-20T11:54:00.000Z", now),
    true,
  );
  assert.equal(
    shadowing.isShadowingStale("ready", "2026-09-20T11:54:00.000Z", now),
    false,
  );
});

test("shadowing progress labels distinguish a delayed job", () => {
  assert.equal(
    shadowing.shadowingProgressLabel("processing", false),
    "Creating pronunciation audio...",
  );
  assert.equal(
    shadowing.shadowingProgressLabel("processing", true),
    "Pronunciation audio is taking longer than expected.",
  );
});

test("details render the shadowing guidance, audio, and retry action", () => {
  const detailsSource = readFileSync("src/components/DetailsScreen.tsx", "utf8");

  assert.match(detailsSource, /Shadowing practice/);
  assert.match(
    detailsSource,
    /Listen, then repeat with the same rhythm and pronunciation\./,
  );
  assert.match(detailsSource, /<audio[\s\S]*?src=\{recording\.shadowingAudioUrl\}/);
  assert.match(detailsSource, /dispatch\(generateShadowingAudio\(recording\.id\)\)/);
});
