import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

test("recording detail uses a wide two-column workbench with mobile reading order", () => {
  const screen = readFileSync("src/components/DetailsScreen.tsx", "utf8");
  const css = readFileSync("app/globals.css", "utf8");
  assert.match(screen, /details-original-material[\s\S]*details-shadowing-material[\s\S]*details-feedback/);
  assert.match(css, /\.main-content\.main-content-recording-detail[\s\S]*max-width:\s*1180px/);
  assert.match(css, /\.details-workbench[\s\S]*grid-template-columns/);
  assert.match(css, /@media \(max-width: 959px\)[\s\S]*\.details-original-material[\s\S]*order:\s*1[\s\S]*\.details-feedback[\s\S]*order:\s*2[\s\S]*\.details-shadowing-material[\s\S]*order:\s*3/);
});

test("both transcripts are collapsible and audio playback is mutually exclusive", () => {
  const screen = readFileSync("src/components/DetailsScreen.tsx", "utf8");
  assert.match(screen, /aria-controls="original-transcript-panel"/);
  assert.match(screen, /aria-controls="corrected-transcript-panel"/);
  assert.match(screen, /shadowingAudioRef\.current\?\.pause\(\)/);
  assert.match(screen, /audioRef\.current\?\.pause\(\)/);
});
