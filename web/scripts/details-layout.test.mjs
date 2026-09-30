import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("recording detail uses one full-width column with mobile reading order", () => {
  const screen = readFileSync("src/components/DetailsScreen.tsx", "utf8");
  const css = readFileSync("app/globals.css", "utf8");
  assert.match(screen, /details-original-material[\s\S]*details-shadowing-material[\s\S]*InterviewRetakes/);
  assert.match(css, /\.main-content\.main-content-recording-detail[\s\S]*max-width:\s*1180px/);
  assert.match(css, /\.details-workbench\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\)/);
  assert.doesNotMatch(css, /\.details-materials\s*\{[^}]*grid-column:\s*2/);
  assert.match(css, /@media \(max-width: 959px\)[\s\S]*\.details-original-material[\s\S]*order:\s*1[\s\S]*\.details-shadowing-material[\s\S]*order:\s*3/);
});

test("both transcripts are collapsible and audio playback is mutually exclusive", () => {
  const screen = readFileSync("src/components/DetailsScreen.tsx", "utf8");
  assert.match(screen, /aria-controls="original-transcript-panel"/);
  assert.match(screen, /aria-controls="corrected-transcript-panel"/);
  assert.match(screen, /shadowingAudioRef\.current\?\.pause\(\)/);
  assert.match(screen, /audioRef\.current\?\.pause\(\)/);
});
