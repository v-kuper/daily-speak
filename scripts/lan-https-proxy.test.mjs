import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const scriptPath = "scripts/setup-lan-https-proxy.ps1";
const docsPath = "docs/LOCAL_WINDOWS_CICD.md";

test("LAN HTTPS proxy script derives its proxy directory from the project checkout", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /\$ProjectRoot\s*=\s*\(Resolve-Path \(Join-Path \$PSScriptRoot "\.\."\)\)\.Path/);
  assert.match(script, /Split-Path -Leaf \$ProjectRoot/);
  assert.match(script, /Join-Path \(Split-Path -Parent \$ProjectRoot\) "\$\(\$ProjectName\)-proxy"/);
  assert.doesNotMatch(script, /C:\\daily-speaking-proxy/i);
});

test("LAN HTTPS proxy script writes Caddy reverse proxy config for the Docker LAN app", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /mkcert -cert-file daily-speaking\.pem -key-file daily-speaking-key\.pem/);
  assert.match(script, /https:\/\/\$\{HostIp\}:\$\{HttpsPort\}/);
  assert.match(script, /reverse_proxy 127\.0\.0\.1:\$AppPort/);
  assert.match(script, /New-NetFirewallRule -DisplayName `?"Daily Speaking HTTPS \$HttpsPort`?"/);
});

test("Windows CI docs describe the exact documented checkout-derived proxy path", () => {
  const docs = readFileSync(docsPath, "utf8");

  assert.match(docs, /D:\\Projects\\daily-speak/);
  assert.match(docs, /D:\\Projects\\daily-speak-proxy/);
  assert.match(docs, /scripts\\setup-lan-https-proxy\.ps1/);
  assert.match(docs, /https:\/\/<windows-ipv4>:3443/);
});
