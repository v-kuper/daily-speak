import assert from "node:assert/strict";
import test from "node:test";

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

test("formatLanSummary prints local, LAN, health, and Windows firewall hints", () => {
  const summary = formatLanSummary({
    port: "8080",
    lanUrls: ["http://192.168.1.42:8080"],
  });

  assert.match(summary, /http:\/\/localhost:8080/);
  assert.match(summary, /http:\/\/192\.168\.1\.42:8080/);
  assert.match(summary, /http:\/\/localhost:8080\/healthz/);
  assert.match(summary, /Microphone recording on LAN\/remote URLs requires HTTPS or localhost/);
  assert.match(summary, /Windows Defender Firewall/);
  assert.match(summary, /PostgreSQL stays inside Docker/);
});
