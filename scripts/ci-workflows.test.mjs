import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const dockerLanScript = readFileSync("scripts/docker-lan.mjs", "utf8");
const dockerfile = readFileSync("Dockerfile", "utf8");
const dockerCompose = readFileSync("docker-compose.yml", "utf8");
const qualityWorkflow = readFileSync(
  ".github/workflows/quality-gates.yml",
  "utf8",
);

test("quality workflow runs the consolidated project quality command", () => {
  assert.match(qualityWorkflow, /run:\s+npm run quality/);
});

test("quality workflow provisions Go and PostgreSQL for smoke API checks", () => {
  assert.match(qualityWorkflow, /uses:\s+actions\/setup-go@v5/);
  assert.match(qualityWorkflow, /go-version-file:\s+backend\/go\.mod/);
  assert.match(qualityWorkflow, /services:\s*\n\s+postgres:/);
  assert.match(qualityWorkflow, /image:\s+postgres:16-alpine/);
  assert.match(
    qualityWorkflow,
    /DATABASE_URL:\s+postgres:\/\/postgres:postgres@127\.0\.0\.1:5432\/daily_speaking/,
  );
});

test("local deploy workflow targets the dedicated Windows self-hosted runner", () => {
  const deployWorkflow = readFileSync(
    ".github/workflows/deploy-local.yml",
    "utf8",
  );

  assert.match(deployWorkflow, /workflow_dispatch:/);
  assert.match(deployWorkflow, /branches:\s*\n\s+- main\s*\n\s+- master/);
  assert.match(deployWorkflow, /shell:\s+powershell/);
  assert.match(
    deployWorkflow,
    /runs-on:\s*\[self-hosted,\s*windows,\s*daily-speaking\]/,
  );
});

test("local deploy workflow verifies quality, deploys the LAN Docker app, and checks health", () => {
  const deployWorkflow = readFileSync(
    ".github/workflows/deploy-local.yml",
    "utf8",
  );

  assert.match(deployWorkflow, /POSTGRES_PORT:\s+\$\{\{\s*vars\.POSTGRES_PORT/);
  assert.match(deployWorkflow, /uses:\s+actions\/setup-go@v5/);
  assert.match(deployWorkflow, /go-version-file:\s+backend\/go\.mod/);
  assert.match(deployWorkflow, /run:\s+npm run quality/);
  assert.match(deployWorkflow, /run:\s+npm run docker:lan/);
  assert.match(deployWorkflow, /\/healthz/);
});

test("local deploy uses a stable Docker Compose project name", () => {
  const deployWorkflow = readFileSync(
    ".github/workflows/deploy-local.yml",
    "utf8",
  );

  assert.match(deployWorkflow, /COMPOSE_PROJECT_NAME:\s+daily-speaking/);
  assert.match(dockerLanScript, /COMPOSE_PROJECT_NAME/);
});

test("repository forces LF endings for scripts used inside Linux containers", () => {
  const gitAttributes = readFileSync(".gitattributes", "utf8");

  assert.match(gitAttributes, /\*\.sh\s+text\s+eol=lf/);
  assert.match(gitAttributes, /Dockerfile\s+text\s+eol=lf/);
});

test("Docker build creates public before copying it into the runtime image", () => {
  assert.match(dockerfile, /RUN mkdir -p public && npm run build/);
  assert.match(dockerfile, /COPY --from=next-build \/app\/public \.\/public/);
});

test("Docker runtime includes the local Python Whisper backend", () => {
  assert.match(dockerfile, /FROM node:22-bookworm-slim AS runtime/);
  assert.match(dockerfile, /python3-venv/);
  assert.match(dockerfile, /ffmpeg/);
  assert.match(dockerfile, /openai-whisper/);
  assert.match(dockerfile, /WHISPER_BACKEND=openai/);
  assert.match(dockerfile, /WHISPER_PYTHON_BIN=\/opt\/whisper\/bin\/python/);
  assert.match(dockerfile, /WHISPER_FFMPEG_BIN=\/usr\/bin\/ffmpeg/);
});

test("Docker Compose defaults to the local Python Whisper backend", () => {
  assert.match(dockerCompose, /WHISPER_BACKEND:\s+\$\{WHISPER_BACKEND:-openai\}/);
  assert.match(
    dockerCompose,
    /WHISPER_PYTHON_BIN:\s+\$\{WHISPER_PYTHON_BIN:-\/opt\/whisper\/bin\/python\}/,
  );
  assert.match(
    dockerCompose,
    /WHISPER_FFMPEG_BIN:\s+\$\{WHISPER_FFMPEG_BIN:-\/usr\/bin\/ffmpeg\}/,
  );
});
