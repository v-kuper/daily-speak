import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import ts from "typescript";

const source = readFileSync("src/lib/interestCatalog.ts", "utf8");
const { outputText } = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
});
const catalog = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

// IDs offered before the catalog was reduced. Each must remain readable from
// profiles saved by older clients, even if its chip is no longer offered.
const originalInterestIds = `
  travel technology fitness business music books movies food sport design career
  languages gaming photography cooking psychology startups marketing productivity
  ai science history nature hiking cycling swimming yoga fashion art architecture
  finance investing crypto pets parenting education philosophy self-development
  culture volunteering entrepreneurship public-speaking remote-work health mindfulness
  news podcasts climate sustainability astronomy space robotics programming
  web-development mobile-development cybersecurity data-science machine-learning
  mathematics physics chemistry biology medicine nutrition mental-health journaling
  minimalism home-decor gardening diy woodworking cars motorcycles aviation sailing
  chess board-games card-games dance theater comedy writing poetry language-teaching
  backpacking luxury-travel coffee tea baking desserts street-food vegan-living
  interior-design real-estate law economics geopolitics social-media content-creation
  audio-production
`.trim().split(/\s+/);

test("the curated catalog has distinct, usable conversation themes", () => {
  const options = catalog.INTEREST_OPTIONS;
  const ids = options.map(({ id }) => id);
  const labels = options.map(({ label }) => label);

  assert.ok(options.length >= 40 && options.length < originalInterestIds.length);
  assert.equal(new Set(ids).size, options.length);
  assert.equal(new Set(labels.map((label) => label.toLowerCase())).size, options.length);
  assert.ok(options.every(({ emoji, label }) => emoji && label.trim() === label && label.length >= 5));
  assert.notEqual(catalog.getInterestOption("food")?.label, catalog.getInterestOption("cooking")?.label);
  assert.notEqual(catalog.getInterestOption("food")?.label, catalog.getInterestOption("nutrition")?.label);
  assert.equal(catalog.getInterestOption("startups"), undefined);
});

test("every original interest ID resolves to a displayed theme", () => {
  const offeredIds = new Set(catalog.INTEREST_OPTIONS.map(({ id }) => id));
  const aliasIds = Object.keys(catalog.LEGACY_INTEREST_ALIASES);

  assert.equal(originalInterestIds.length, 100);
  assert.deepEqual(
    new Set([...offeredIds, ...aliasIds]),
    new Set(originalInterestIds),
  );
  assert.ok(aliasIds.every((id) => !offeredIds.has(id) && offeredIds.has(catalog.LEGACY_INTEREST_ALIASES[id])));
  assert.ok(originalInterestIds.every((id) => catalog.normalizeInterestIds([id]).length === 1));
});

test("saved legacy IDs migrate before deduplication and selection limit", () => {
  assert.deepEqual(
    catalog.normalizeInterestIds([
      "  STARTUPS ", "business", "machine-learning", "ai", "backpacking",
      "travel", "law", "geopolitics", null, "unknown", "books",
    ]),
    ["business", "ai", "travel", "law", "geopolitics", "books"],
  );
  assert.deepEqual(catalog.normalizeInterestIds(["design", "art", "culture"]), ["design", "art"]);
  assert.deepEqual(catalog.normalizeInterestIds(["baking", "cooking", "chess", "card-games"]), ["cooking", "board-games"]);
  assert.equal(catalog.normalizeInterestIds(originalInterestIds).length, catalog.MAX_SELECTED_INTERESTS);
  assert.deepEqual(catalog.normalizeInterestIds("travel"), []);
});

test("distinct saved interests keep their own topic after migration", () => {
  const preciseIds = `design marketing data-science minimalism aviation sailing real-estate
    law audio-production medicine physics chemistry biology geopolitics journaling
    mathematics economics`.trim().split(/\s+/);

  assert.ok(preciseIds.every((id) => catalog.getInterestOption(id)));
  assert.ok(preciseIds.every((id) => !Object.hasOwn(catalog.LEGACY_INTEREST_ALIASES, id)));
  assert.deepEqual(catalog.normalizeInterestIds(["news", "law", "geopolitics"]), ["news", "law", "geopolitics"]);
});

test("AI receives canonical, readable labels for migrated selections", () => {
  assert.deepEqual(
    catalog.resolveInterestLabels(["luxury-travel", "travel", "web-development", "unknown"]),
    ["Travel and places", "Writing software"],
  );
});
