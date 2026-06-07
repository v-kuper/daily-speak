import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_APP_PORT = "3218";
const DEFAULT_COMPOSE_PROJECT_NAME = "daily-speaking";

export function getHostPort(env = process.env) {
  const configuredPort = env.APP_PORT;
  return typeof configuredPort === "string" && configuredPort.trim()
    ? configuredPort.trim()
    : DEFAULT_APP_PORT;
}

export function getComposeProjectName(env = process.env) {
  const configuredName = env.COMPOSE_PROJECT_NAME;
  return typeof configuredName === "string" && configuredName.trim()
    ? configuredName.trim()
    : DEFAULT_COMPOSE_PROJECT_NAME;
}

function isIPv4Address(entry) {
  return entry.family === "IPv4" || entry.family === 4;
}

function isPrivateNetworkAddress(address) {
  const octets = address.split(".").map((part) => Number(part));
  if (
    octets.length !== 4 ||
    octets.some((octet) => !Number.isInteger(octet) || octet < 0 || octet > 255)
  ) {
    return false;
  }

  const [first, second] = octets;
  return (
    first === 10 ||
    (first === 172 && second >= 16 && second <= 31) ||
    (first === 192 && second === 168) ||
    (first === 100 && second >= 64 && second <= 127)
  );
}

function isLanAddress(entry) {
  return (
    entry &&
    isIPv4Address(entry) &&
    !entry.internal &&
    typeof entry.address === "string" &&
    isPrivateNetworkAddress(entry.address)
  );
}

export function listLanUrls({
  interfaces = os.networkInterfaces(),
  port = getHostPort(),
} = {}) {
  const urls = [];

  for (const entries of Object.values(interfaces)) {
    for (const entry of entries ?? []) {
      if (isLanAddress(entry)) {
        urls.push(`http://${entry.address}:${port}`);
      }
    }
  }

  return [...new Set(urls)].sort();
}

export function formatLanSummary({
  port = getHostPort(),
  lanUrls = listLanUrls({ port }),
} = {}) {
  const lines = [
    "",
    "Daily Speaking Practice is running in Docker.",
    `Local:  http://localhost:${port}`,
  ];

  if (lanUrls.length > 0) {
    lines.push("LAN:");
    for (const url of lanUrls) {
      lines.push(`  ${url}`);
    }
  } else {
    lines.push("LAN:    no non-internal IPv4 address detected");
    lines.push(`        On Windows, run ipconfig and open http://<IPv4>:${port}`);
  }

  lines.push(`Health: http://localhost:${port}/healthz`);
  lines.push("");
  lines.push(
    `For Windows LAN access, allow inbound TCP port ${port} in Windows Defender Firewall / Docker Desktop if another device cannot connect.`,
  );
  lines.push(
    "PostgreSQL stays inside Docker; use the app URL, not the database port, from other devices.",
  );

  return lines.join(os.EOL);
}

function runDockerCompose({ port, env = process.env } = {}) {
  const composeProjectName = getComposeProjectName(env);

  return new Promise((resolve) => {
    const child = spawn(
      "docker",
      ["compose", "up", "--build", "-d", "app", "postgres"],
      {
        env: { ...env, APP_PORT: port, COMPOSE_PROJECT_NAME: composeProjectName },
        stdio: "inherit",
      },
    );

    child.on("error", (error) => {
      console.error(`Failed to start Docker Compose: ${error.message}`);
      resolve(1);
    });

    child.on("close", (code) => {
      resolve(code ?? 1);
    });
  });
}

function printHelp() {
  console.log(`Usage: npm run docker:lan

Builds and starts the app and PostgreSQL with Docker Compose, then prints
local-network URLs for this machine.

Environment:
  APP_PORT              Host port to expose, default ${DEFAULT_APP_PORT}
  COMPOSE_PROJECT_NAME  Docker Compose project name, default ${DEFAULT_COMPOSE_PROJECT_NAME}

Options:
  --print-only  Print URLs without starting Docker
`);
}

export async function main(argv = process.argv.slice(2), env = process.env) {
  const port = getHostPort(env);

  if (argv.includes("--help") || argv.includes("-h")) {
    printHelp();
    return 0;
  }

  if (argv.includes("--print-only")) {
    console.log(formatLanSummary({ port }));
    return 0;
  }

  console.log(`Building and starting Docker app on host port ${port}...`);
  const exitCode = await runDockerCompose({ port, env });
  if (exitCode !== 0) {
    return exitCode;
  }

  console.log(formatLanSummary({ port }));
  return 0;
}

const currentFile = fileURLToPath(import.meta.url);
const invokedFile = process.argv[1] ? path.resolve(process.argv[1]) : "";

if (invokedFile === currentFile) {
  process.exitCode = await main();
}
