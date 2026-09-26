import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_APP_PORT = "3218";
const DEFAULT_API_PORT = "3219";
const DEFAULT_COMPOSE_PROJECT_NAME = "daily-speaking";

export function getHostPort(env = process.env) {
  const configuredPort = env.APP_PORT;
  return typeof configuredPort === "string" && configuredPort.trim()
    ? configuredPort.trim()
    : DEFAULT_APP_PORT;
}

function getApiPort(env = process.env) {
  return env.API_PORT?.trim() || DEFAULT_API_PORT;
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
  return listLanAddresses(interfaces).map((address) => `http://${address}:${port}`).sort();
}

export function listLanAddresses(interfaces = os.networkInterfaces()) {
  const addresses = [];
  for (const [name, entries] of Object.entries(interfaces)) {
    for (const entry of entries ?? []) {
      if (isLanAddress(entry)) {
        const virtual = /docker|veth|wsl|virtual|vmware|vbox|hyper-v|utun|tun\d|tap\d|tailscale|wireguard|^wg\d|^br-/i.test(name);
        const networkRank = entry.address.startsWith("192.168.") ? 0
          : entry.address.startsWith("10.") ? 1 : 2;
        addresses.push({ address: entry.address, rank: (virtual ? 10 : 0) + networkRank });
      }
    }
  }
  addresses.sort((a, b) => a.rank - b.rank || a.address.localeCompare(b.address));
  return [...new Set(addresses.map(({ address }) => address))];
}

export function formatLanSummary({
  webPort = getHostPort(),
  apiPort = getApiPort(),
  lanAddresses = listLanAddresses(),
} = {}) {
  const hostAddress = lanAddresses[0] ?? "localhost";
  const lines = [
    "",
    "Daily Speaking Practice Docker endpoints:",
    `Web:     http://${hostAddress}:${webPort}`,
    `API:     http://${hostAddress}:${apiPort}`,
    `Health:  http://${hostAddress}:${apiPort}/healthz`,
    `Swagger: http://${hostAddress}:${apiPort}/docs`,
  ];

  if (lanAddresses.length === 0) {
    lines.push("LAN:    no non-internal IPv4 address detected");
    lines.push("        On Windows, run ipconfig and check the LAN adapter before deploying again.");
  }

  lines.push("");
  lines.push(
    `For Windows LAN access, allow inbound TCP ports ${webPort} and ${apiPort} in Windows Defender Firewall / Docker Desktop if another device cannot connect.`,
  );
  lines.push(
    "Microphone recording on LAN/remote URLs requires HTTPS or localhost; plain HTTP IP addresses can load the app but cannot show the browser microphone permission prompt.",
  );
  lines.push(
    "PostgreSQL stays inside Docker; use the app URL, not the database port, from other devices.",
  );

  return lines.join(os.EOL);
}

export function buildComposeCommand({ env = process.env, interfaces = os.networkInterfaces() } = {}) {
  const webPort = getHostPort(env);
  const apiPort = getApiPort(env);
  const hostAddress = listLanAddresses(interfaces)[0] ?? "localhost";
  return {
    command: "docker",
    args: ["compose", "up", "--build", "-d", "--remove-orphans", "web", "backend", "worker", "postgres"],
    env: {
      ...env,
      APP_PORT: webPort,
      API_PORT: apiPort,
      COMPOSE_PROJECT_NAME: getComposeProjectName(env),
      PUBLIC_API_BASE_URL: `http://${hostAddress}:${apiPort}`,
      CORS_ALLOWED_ORIGINS: [...new Set([
        `http://${hostAddress}:${webPort}`,
        `http://${hostAddress}:${apiPort}`,
      ])].join(","),
    },
  };
}

function runDockerCompose({ command, args, env }) {
  return new Promise((resolve) => {
    const child = spawn(
      command,
      args,
      {
        env,
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

Builds and starts web, backend, and PostgreSQL with Docker Compose, then prints
local-network URLs for this machine.

Environment:
  APP_PORT              Web host port, default ${DEFAULT_APP_PORT}
  API_PORT              API host port, default ${DEFAULT_API_PORT}
  COMPOSE_PROJECT_NAME  Docker Compose project name, default ${DEFAULT_COMPOSE_PROJECT_NAME}

Options:
  --print-only  Print URLs without starting Docker
`);
}

export async function main(argv = process.argv.slice(2), env = process.env) {
  const interfaces = os.networkInterfaces();
  const summaryOptions = {
    webPort: getHostPort(env), apiPort: getApiPort(env),
    lanAddresses: listLanAddresses(interfaces),
  };

  if (argv.includes("--help") || argv.includes("-h")) {
    printHelp();
    return 0;
  }

  if (argv.includes("--print-only")) {
    console.log(formatLanSummary(summaryOptions));
    return 0;
  }

  console.log(`Building and starting Docker web on port ${summaryOptions.webPort} and API on port ${summaryOptions.apiPort}...`);
  const exitCode = await runDockerCompose(buildComposeCommand({ env, interfaces }));
  if (exitCode !== 0) {
    return exitCode;
  }

  console.log(formatLanSummary(summaryOptions));
  return 0;
}

const currentFile = fileURLToPath(import.meta.url);
const invokedFile = process.argv[1] ? path.resolve(process.argv[1]) : "";

if (invokedFile === currentFile) {
  process.exitCode = await main();
}
