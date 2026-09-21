import assert from "node:assert/strict";
import test from "node:test";
import * as lan from "./docker-lan.mjs";

import {
  formatLanSummary,
  getComposeProjectName,
  getHostPort,
  listLanUrls,
} from "./docker-lan.mjs";

test("getHostPort defaults to the Docker Compose app port and accepts overrides", () => {
  assert.equal(getHostPort({}), "3218");
  assert.equal(getHostPort({ APP_PORT: "8080" }), "8080");
});

test("getComposeProjectName defaults to the stable project name and accepts overrides", () => {
  assert.equal(getComposeProjectName({}), "daily-speaking");
  assert.equal(getComposeProjectName({ COMPOSE_PROJECT_NAME: "custom-app" }), "custom-app");
});

test("listLanUrls returns only private LAN IPv4 addresses", () => {
  const urls = listLanUrls({
    port: "3218",
    interfaces: {
      Loopback: [{ family: "IPv4", address: "127.0.0.1", internal: true }],
      Ethernet: [{ family: "IPv4", address: "192.168.1.42", internal: false }],
      WiFi: [{ family: "IPv4", address: "10.0.0.8", internal: false }],
      LinkLocal: [{ family: "IPv4", address: "169.254.12.1", internal: false }],
      Reserved: [{ family: "IPv4", address: "240.0.0.2", internal: false }],
      IPv6: [{ family: "IPv6", address: "fe80::1", internal: false }],
    },
  });

  assert.deepEqual(urls, [
    "http://10.0.0.8:3218",
    "http://192.168.1.42:3218",
  ]);
});

test("LAN summary reports independent web, API, health, and Swagger URLs", () => {
  const summary = formatLanSummary({
    webPort: "8080",
    apiPort: "8081",
    lanAddresses: ["192.168.1.42"],
  });

  assert.match(summary, /http:\/\/localhost:8080/);
  assert.match(summary, /Web:\s+http:\/\/192\.168\.1\.42:8080/);
  assert.match(summary, /API:\s+http:\/\/192\.168\.1\.42:8081/);
  assert.match(summary, /Health:\s+http:\/\/192\.168\.1\.42:8081\/healthz/);
  assert.match(summary, /Swagger:\s+http:\/\/192\.168\.1\.42:8081\/docs/);
  assert.match(summary, /Microphone recording on LAN\/remote URLs requires HTTPS or localhost/);
  assert.match(summary, /Windows Defender Firewall/);
  assert.match(summary, /PostgreSQL stays inside Docker/);
});

test("HTTP deployment selects a physical LAN adapter and preserves provider/storage configuration", () => {
  assert.equal(typeof lan.buildComposeCommand, "function");
  const env = {
    APP_PORT: " 8080 ", API_PORT: " 8081 ", COMPOSE_PROJECT_NAME: "custom-app",
    CARTESIA_API_KEY: "test-key", WHISPER_LANGUAGE: "en", UPLOADS_HOST_DIR: "D:\\media",
    PUBLIC_API_BASE_URL: "https://stale.example", CORS_ALLOWED_ORIGINS: "*",
  };
  const command = lan.buildComposeCommand({ env, interfaces: {
    "vEthernet (WSL)": [{ family: "IPv4", address: "192.168.0.1", internal: false }],
    Docker: [{ family: "IPv4", address: "172.17.0.1", internal: false }],
    utun4: [{ family: "IPv4", address: "10.0.0.1", internal: false }],
    WiFi: [{ family: "IPv4", address: "10.1.2.3", internal: false }],
  } });
  assert.equal(command.command, "docker");
  assert.deepEqual(command.args, ["compose", "up", "--build", "-d", "web", "backend", "postgres"]);
  assert.deepEqual(command.env, {
    ...env, APP_PORT: "8080", API_PORT: "8081",
    PUBLIC_API_BASE_URL: "http://10.1.2.3:8081",
    CORS_ALLOWED_ORIGINS: "http://10.1.2.3:8080,http://localhost:8080,http://127.0.0.1:8080",
  });
  assert.equal(env.PUBLIC_API_BASE_URL, "https://stale.example");
});

test("HTTP deployment defaults to separate ports and loopback when LAN detection is empty", () => {
  assert.equal(typeof lan.buildComposeCommand, "function");
  const { env } = lan.buildComposeCommand({ env: {}, interfaces: {} });
  assert.equal(env.APP_PORT, "3218");
  assert.equal(env.API_PORT, "3219");
  assert.equal(env.COMPOSE_PROJECT_NAME, "daily-speaking");
  assert.equal(env.PUBLIC_API_BASE_URL, "http://localhost:3219");
  assert.equal(env.CORS_ALLOWED_ORIGINS, "http://localhost:3218,http://127.0.0.1:3218");
  const summary = formatLanSummary({ webPort: "3218", apiPort: "3219", lanAddresses: [] });
  assert.match(summary, /no .*IPv4 address detected/);
  assert.match(summary, /API:\s+http:\/\/localhost:3219/);
});

test("LAN preference orders physical addresses before virtual interfaces and deduplicates them", () => {
  assert.equal(typeof lan.listLanAddresses, "function");
  assert.deepEqual(lan.listLanAddresses({
    "vEthernet (WSL)": [{ family: 4, address: "192.168.0.1", internal: false }],
    Ethernet: [{ family: 4, address: "10.0.0.8", internal: false }],
    WiFi: [{ family: 4, address: "192.168.1.42", internal: false }, { family: 4, address: "192.168.1.42", internal: false }],
    Loopback: [{ family: 4, address: "127.0.0.1", internal: true }],
  }), ["192.168.1.42", "10.0.0.8", "192.168.0.1"]);
});
