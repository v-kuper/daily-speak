import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

test("Feed and legacy sharing components are absent from the web product", () => {
  for (const component of ["FeedScreen", "FeedThreadScreen", "FeedReactionBar", "ShareModal", "ShareScreen"]) {
    assert.equal(existsSync(`src/components/${component}.tsx`), false, `${component} should be deleted`);
  }
});

// This inventory enforces the intentionally forbidden client product surface.
// Backend behavior and its contract are covered by the backend's own suites.
test("web runtime inventory contains no Feed network, state, navigation, or sharing surface", () => {
  const forbidden = /\b(?:\w*Feed\w*|feed[A-Z]\w*|FEED_\w*|ShareAction|ShareModal|ShareScreen|\w*Share(?:Modal|Preview|Link)|shareModalOpen|shareAction|copyMessage|setCopyMessage)\b|\/api\/feed\b|["'](?:feed|feedThread|share)["']|\.(?:feed|share)-/;
  const inspect = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) inspect(path);
      else if (/\.(?:tsx?|css)$/.test(path)) {
        assert.doesNotMatch(readFileSync(path, "utf8"), forbidden, `${path} contains a forbidden client surface`);
      }
    }
  };
  inspect("src");
  inspect("app");
});
