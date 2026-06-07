import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const scriptPath = "scripts/setup-lan-https-proxy.ps1";
const docsPath = "docs/LOCAL_WINDOWS_CICD.md";
const composePath = "docker-compose.yml";

test("LAN HTTPS proxy script derives its certificate directory from the project checkout", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /\$ProjectRoot\s*=\s*\(Resolve-Path \(Join-Path \$PSScriptRoot "\.\."\)\)\.Path/);
  assert.match(script, /Join-Path \$ProjectRoot "lan-https"/);
  assert.match(script, /Join-Path \$HttpsDir "certs"/);
  assert.match(script, /winget install --id FiloSottile\.mkcert/);
  assert.doesNotMatch(script, /C:\\daily-speaking-proxy/i);
});

test("LAN HTTPS proxy script writes Caddy reverse proxy config for the Docker app service", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /mkcert -cert-file \$certPath -key-file \$keyPath/);
  assert.match(script, /https:\/\/\$\{HostIp\}:\$\{HttpsPort\}/);
  assert.match(script, /https:\/\/127\.0\.0\.1:\$\{HttpsPort\}/);
  assert.match(script, /reverse_proxy app:3000/);
  assert.match(script, /docker compose up --build -d app postgres lan-https/);
  assert.match(script, /function Ensure-FirewallRule/);
  assert.match(script, /Get-NetFirewallRule -DisplayName \$displayName/);
  assert.match(script, /New-NetFirewallRule -DisplayName \$displayName/);
  assert.match(script, /New-NetFirewallRule -DisplayName `?"Daily Speaking HTTPS \$HttpsPort`?"/);
});

test("LAN HTTPS proxy script keeps deployment running when firewall rule creation is denied", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /try\s*\{\s*New-NetFirewallRule -DisplayName \$displayName/s);
  assert.match(script, /catch\s*\{/);
  assert.match(script, /Write-Warning/);
  assert.match(script, /pre-create the same rule/);
});

test("LAN HTTPS proxy script prefers physical LAN addresses over Docker or WSL adapters", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /Sort-Object\s+\{\s*if\s*\(\$_.IPAddress -match "\^192\\\.168\\\."\)/);
  assert.match(script, /elseif\s*\(\$_.IPAddress -match "\^10\\\."\)/);
  assert.match(script, /else\s*\{\s*2\s*\}/);
});

test("Docker Compose defines the LAN HTTPS Caddy service", () => {
  const compose = readFileSync(composePath, "utf8");

  assert.match(compose, /lan-https:/);
  assert.match(compose, /image:\s+caddy:2-alpine/);
  assert.match(compose, /\$\{HTTPS_PORT:-3443\}:3443/);
  assert.match(compose, /\.\/lan-https\/Caddyfile:\/etc\/caddy\/Caddyfile:ro/);
  assert.match(compose, /\.\/lan-https\/certs:\/certs:ro/);
});

test("Windows CI docs describe the exact Docker HTTPS setup path", () => {
  const docs = readFileSync(docsPath, "utf8");

  assert.match(docs, /D:\\Projects\\daily-speak/);
  assert.match(docs, /D:\\Projects\\daily-speak\\lan-https/);
  assert.match(docs, /scripts\\setup-lan-https-proxy\.ps1/);
  assert.match(docs, /https:\/\/<windows-ipv4>:3443/);
  assert.match(docs, /docker compose up --build -d app postgres lan-https/);
});
