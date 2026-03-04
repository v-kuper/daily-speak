#!/usr/bin/env node
import process from "node:process";
import { spawn } from "node:child_process";

const port = Number.parseInt(process.env.SMOKE_PORT ?? "3217", 10);
const baseUrl = `http://127.0.0.1:${port}`;
const npmCmd = process.platform === "win32" ? "npm.cmd" : "npm";
const startupTimeoutMs = 120_000;
const pollIntervalMs = 1_500;
const expectedChecks = [
  {
    name: "auth/session unauthorized",
    method: "GET",
    path: "/api/auth/session",
    expectedStatus: 401
  },
  {
    name: "auth/register validation",
    method: "POST",
    path: "/api/auth/register",
    body: { email: "bad-email", password: "123" },
    expectedStatus: 400
  },
  {
    name: "auth/login validation",
    method: "POST",
    path: "/api/auth/login",
    body: { email: "bad-email", password: "123" },
    expectedStatus: 400
  },
  {
    name: "daily-questions requires date",
    method: "GET",
    path: "/api/daily-questions",
    expectedStatus: 400
  },
  {
    name: "topic-guidance requires topic",
    method: "GET",
    path: "/api/topic-guidance",
    expectedStatus: 400
  },
  {
    name: "user/data unauthorized",
    method: "GET",
    path: "/api/user/data",
    expectedStatus: 401
  },
  {
    name: "user/recordings unauthorized",
    method: "POST",
    path: "/api/user/recordings",
    body: { recording: {} },
    expectedStatus: 401
  }
];

const logs = [];

const pushLog = (source, chunk) => {
  const lines = chunk
    .toString("utf8")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .slice(0, 5);

  for (const line of lines) {
    logs.push(`[${source}] ${line}`);
    if (logs.length > 200) {
      logs.shift();
    }
  }
};

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const waitForServer = async () => {
  const startedAt = Date.now();

  while (Date.now() - startedAt < startupTimeoutMs) {
    try {
      const response = await fetch(`${baseUrl}/api/auth/session`, { method: "GET" });
      if (response.status === 401 || response.status === 200) {
        return;
      }
    } catch {
      // keep polling
    }

    await wait(pollIntervalMs);
  }

  throw new Error(`Server did not become ready in ${startupTimeoutMs}ms.`);
};

const runChecks = async () => {
  for (const check of expectedChecks) {
    const response = await fetch(`${baseUrl}${check.path}`, {
      method: check.method,
      headers: check.body ? { "Content-Type": "application/json" } : undefined,
      body: check.body ? JSON.stringify(check.body) : undefined
    });

    if (response.status !== check.expectedStatus) {
      const responseBody = await response.text();
      throw new Error(
        `${check.name} failed: expected ${check.expectedStatus}, got ${response.status}. Body: ${responseBody.slice(0, 300)}`
      );
    }

    process.stdout.write(`✓ ${check.name}\n`);
  }
};

const stopServer = async (child) => {
  if (!child || child.killed) {
    return;
  }

  child.kill("SIGTERM");

  const exited = await new Promise((resolve) => {
    const timeout = setTimeout(() => {
      resolve(false);
    }, 8_000);

    child.once("exit", () => {
      clearTimeout(timeout);
      resolve(true);
    });
  });

  if (!exited) {
    child.kill("SIGKILL");
  }
};

const main = async () => {
  const server = spawn(npmCmd, ["run", "dev", "--", "--port", String(port)], {
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...process.env,
      CI: "1",
      NEXT_TELEMETRY_DISABLED: "1"
    }
  });

  server.stdout.on("data", (chunk) => pushLog("dev:stdout", chunk));
  server.stderr.on("data", (chunk) => pushLog("dev:stderr", chunk));

  let serverExitCode = null;
  server.on("exit", (code) => {
    serverExitCode = code;
  });

  try {
    await waitForServer();
    await runChecks();
  } catch (error) {
    const details = logs.slice(-40).join("\n");
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`${message}\n\nRecent dev logs:\n${details}`);
  } finally {
    await stopServer(server);
  }

  if (serverExitCode !== null && serverExitCode !== 0 && serverExitCode !== 130) {
    throw new Error(`Dev server exited with code ${serverExitCode}.`);
  }

  process.stdout.write("Smoke API checks passed.\n");
};

main().catch((error) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});
