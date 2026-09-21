import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
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

test("web build traces are rooted in the standalone web project", () => {
  const config = readFileSync("web/next.config.ts", "utf8");
  assert.match(config, /outputFileTracingRoot:\s*projectRoot/);
  assert.match(config, /fileURLToPath\(import\.meta\.url\)/);
});

test("standalone backend upload defaults are ignored by Git", () => {
  for (const path of [
    "backend/public/uploads/recordings/user/recording.webm",
    "backend/public/uploads/feed-replies/user/reply.webm",
    "backend/public/uploads/shadowing/user/pronunciation.mp3",
  ]) {
    const result = spawnSync("git", ["check-ignore", "--quiet", "--no-index", path]);
    assert.equal(result.status, 0, `${path} must be ignored by the repository Git configuration`);
  }
});

test("backend environment example exposes the supported Whisper initial prompt", () => {
  const env = Object.fromEntries(readFileSync("backend/.env.example", "utf8")
    .split(/\r?\n/)
    .filter((line) => line && !line.startsWith("#") && line.includes("="))
    .map((line) => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1).replace(/^"|"$/g, "")];
    }));
  const whisper = readFileSync("backend/internal/transcription/whisper.go", "utf8");

  assert.match(whisper, /os\.Getenv\("WHISPER_INITIAL_PROMPT"\)/);
  assert.match(env.WHISPER_INITIAL_PROMPT ?? "", /English.*Russian.*Cyrillic/);
});

test("backend source has no Next.js upstream", () => {
  const main = readFileSync("backend/cmd/api/main.go", "utf8");
  const server = readFileSync("backend/internal/httpapi/server.go", "utf8");
  const smoke = readFileSync("scripts/smoke-api.mjs", "utf8");
  assert.doesNotMatch(main, /NEXT_UPSTREAM_URL|NextURL|proxying Next/);
  assert.doesNotMatch(server, /httputil|NewSingleHostReverseProxy|nextProxy|NextURL/);
  assert.doesNotMatch(smoke, /NEXT_|next/i);
  assert.match(smoke, /cwd:\s*"backend"/);
});
