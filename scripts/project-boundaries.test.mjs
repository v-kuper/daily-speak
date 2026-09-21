import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

test("web and backend are standalone projects", () => {
  for (const path of [
    "web/package.json",
    "web/package-lock.json",
    "web/app/layout.tsx",
    "web/src/store/index.ts",
    "web/Dockerfile",
    "backend/go.mod",
    "backend/Dockerfile",
  ]) {
    assert.equal(existsSync(path), true, `missing ${path}`);
  }
});

test("root package only orchestrates independent projects", () => {
  const rootPackage = JSON.parse(readFileSync("package.json", "utf8"));
  assert.deepEqual(rootPackage.dependencies ?? {}, {});
  assert.deepEqual(rootPackage.devDependencies ?? {}, {});
  assert.match(rootPackage.scripts.quality, /web:quality/);
  assert.match(rootPackage.scripts.quality, /backend:test/);
});

test("frontend source is not left at repository root", () => {
  assert.equal(existsSync("app"), false);
  assert.equal(existsSync("src"), false);
  assert.equal(existsSync("next.config.ts"), false);
});
