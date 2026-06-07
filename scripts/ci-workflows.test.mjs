import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const qualityWorkflow = readFileSync(
  ".github/workflows/quality-gates.yml",
  "utf8",
);

test("quality workflow runs the consolidated project quality command", () => {
  assert.match(qualityWorkflow, /run:\s+npm run quality/);
});

test("local deploy workflow targets the dedicated Windows self-hosted runner", () => {
  const deployWorkflow = readFileSync(
    ".github/workflows/deploy-local.yml",
    "utf8",
  );

  assert.match(deployWorkflow, /workflow_dispatch:/);
  assert.match(deployWorkflow, /branches:\s*\n\s+- main\s*\n\s+- master/);
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
