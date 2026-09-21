import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import test from "node:test";

const { load: parseYaml } = createRequire(new URL("../web/package.json", import.meta.url))("js-yaml");

const scriptPath = "scripts/setup-lan-https-proxy.ps1";
const composePath = "docker-compose.yml";

test("LAN HTTPS proxy script derives its certificate directory from the project checkout", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /\$ProjectRoot\s*=\s*\(Resolve-Path \(Join-Path \$PSScriptRoot "\.\."\)\)\.Path/);
  assert.match(script, /Join-Path \$ProjectRoot "lan-https"/);
  assert.match(script, /Join-Path \$HttpsDir "certs"/);
  assert.match(script, /winget install --id FiloSottile\.mkcert/);
  assert.doesNotMatch(script, /C:\\daily-speaking-proxy/i);
});

test("LAN HTTPS proxy script writes independent Caddy sites and starts separate services", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /mkcert -cert-file \$certPath -key-file \$keyPath/);
  assert.match(script, /https:\/\/\$\{HostIp\}:\$\{HttpsPort\}/);
  assert.match(script, /https:\/\/127\.0\.0\.1:\$\{HttpsPort\}/);
  assert.match(script, /reverse_proxy web:3000/);
  assert.match(script, /reverse_proxy backend:3000/);
  assert.doesNotMatch(script, /reverse_proxy app:|handle_path/);
  assert.match(script, /docker compose up --build -d web backend postgres lan-https/);
  assert.match(script, /function Ensure-FirewallRule/);
  assert.match(script, /Get-NetFirewallRule -DisplayName \$displayName/);
  assert.match(script, /New-NetFirewallRule -DisplayName \$displayName/);
  assert.match(script, /New-NetFirewallRule -DisplayName `?"Daily Speaking HTTPS \$HttpsPort`?"/);
  assert.match(script, /Ensure-FirewallRule -Port \$HttpsPort/);
  assert.match(script, /Ensure-FirewallRule -Port \$ApiHttpsPort/);
  assert.match(script, /-LocalPort \$Port -Action Allow/);
  assert.match(script, /mkcert -cert-file \$certPath -key-file \$keyPath \$HostIp localhost 127\.0\.0\.1/);
});

test("LAN HTTPS proxy script keeps deployment running when firewall rule creation is denied", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /try\s*\{\s*New-NetFirewallRule -DisplayName \$displayName/s);
  assert.match(script, /catch\s*\{/);
  assert.match(script, /Write-Warning/);
  assert.match(script, /pre-create the same rule/);
});

test("LAN HTTPS proxy script prepares persistent uploaded media storage", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /\[string\]\$UploadsHostDir/);
  assert.match(script, /UPLOADS_HOST_DIR/);
  assert.match(script, /New-Item -ItemType Directory -Force \$UploadsHostDir/);
  assert.match(script, /\$env:UPLOADS_HOST_DIR = \$UploadsHostDir/);
  assert.match(script, /\$env:UPLOADS_DIR = "\/app\/uploads"/);
});

test("LAN HTTPS proxy script prefers physical LAN addresses over Docker or WSL adapters", () => {
  const script = readFileSync(scriptPath, "utf8");

  assert.match(script, /Get-NetAdapter -Physical/);
  assert.match(script, /Sort-Object\s+\{\s*if\s*\(\$physicalInterfaces -contains \$_.InterfaceIndex\)/);
  assert.match(script, /if\s*\(\$_.IPAddress -match "\^192\\\.168\\\."\)/);
  assert.match(script, /elseif\s*\(\$_.IPAddress -match "\^10\\\."\)/);
  assert.match(script, /else\s*\{\s*2\s*\}/);
  assert.match(script, /\$addresses = @\(\s*Get-NetIPAddress/);
  assert.match(script, /Select-Object -ExpandProperty IPAddress -Unique\s*\)/);
});

test("generated Caddy listeners and Compose mappings agree for default and overridden ports", () => {
  const script = readFileSync(scriptPath, "utf8");
  const template = script.match(/\$caddyfile = @"\r?\n([\s\S]*?)\r?\n"@/)?.[1];
  assert.ok(template, "Caddy here-string is required");
  for (const [webPort, apiPort, expectedPorts] of [
    ["3443", "3444", ["0.0.0.0:3443:3443", "0.0.0.0:3444:3444"]],
    ["8443", "8444", ["0.0.0.0:8443:8443", "0.0.0.0:8444:8444"]],
  ]) {
    const values = { HostIp: "192.168.1.42", HttpsPort: webPort, ApiHttpsPort: apiPort };
    const rendered = template.replace(/\$\{(\w+)\}/g, (_, key) => {
      assert.ok(Object.hasOwn(values, key), `unexpected template variable ${key}`);
      return values[key];
    });
    const sites = [...rendered.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, addresses, body]) => ({
      addresses: addresses.trim().split(/,\s*/),
      upstream: body.match(/reverse_proxy (\S+)/)?.[1],
      tls: body.match(/tls (.+)/)?.[1],
    }));
    assert.deepEqual(sites, [
      { addresses: [`https://192.168.1.42:${webPort}`, `https://localhost:${webPort}`, `https://127.0.0.1:${webPort}`], upstream: "web:3000", tls: "/certs/daily-speaking.pem /certs/daily-speaking-key.pem" },
      { addresses: [`https://192.168.1.42:${apiPort}`, `https://localhost:${apiPort}`, `https://127.0.0.1:${apiPort}`], upstream: "backend:3000", tls: "/certs/daily-speaking.pem /certs/daily-speaking-key.pem" },
    ]);
    const env = webPort === "3443" ? {} : { HTTPS_PORT: webPort, API_HTTPS_PORT: apiPort };
    const compose = parseYaml(readFileSync(composePath, "utf8").replace(/\$\{(\w+):-([^}]*)\}/g, (_, key, fallback) => env[key] || fallback));
    assert.deepEqual(compose.services["lan-https"].ports, expectedPorts);
    assert.deepEqual(compose.services["lan-https"].depends_on, ["web", "backend"]);
    assert.equal(compose.services["lan-https"].image, "caddy:2-alpine");
    assert.deepEqual(compose.services["lan-https"].volumes, ["./lan-https/Caddyfile:/etc/caddy/Caddyfile:ro", "./lan-https/certs:/certs:ro"]);
  }
});

test("HTTPS setup supplies runtime origins and secure cookies before Compose starts", () => {
  const script = readFileSync(scriptPath, "utf8");
  for (const [name, variable, fallback] of [["AppPort", "APP_PORT", 3218], ["ApiPort", "API_PORT", 3219], ["HttpsPort", "HTTPS_PORT", 3443], ["ApiHttpsPort", "API_HTTPS_PORT", 3444]]) {
    assert.ok(script.includes(`[int]$${name} = $(if ($env:${variable}) { [int]$env:${variable} } else { ${fallback} })`));
  }
  const beforeCompose = script.split("docker compose up --build -d")[0];
  const variables = Object.fromEntries([...beforeCompose.matchAll(/\$env:(\w+) = "([^"\n]*)"/g)].map(([, name, value]) => [name, value]));
  assert.deepEqual(Object.fromEntries(["APP_PORT", "API_PORT", "HTTPS_PORT", "API_HTTPS_PORT", "PUBLIC_API_BASE_URL", "CORS_ALLOWED_ORIGINS", "SESSION_COOKIE_SECURE", "SESSION_COOKIE_SAME_SITE"].map((name) => [name, variables[name]])), {
    APP_PORT: "$AppPort", API_PORT: "$ApiPort", HTTPS_PORT: "$HttpsPort", API_HTTPS_PORT: "$ApiHttpsPort",
    PUBLIC_API_BASE_URL: "https://${HostIp}:${ApiHttpsPort}",
    CORS_ALLOWED_ORIGINS: "https://${HostIp}:${HttpsPort},https://localhost:${HttpsPort},https://127.0.0.1:${HttpsPort},http://${HostIp}:${AppPort},http://localhost:${AppPort},http://127.0.0.1:${AppPort}",
    SESSION_COOKIE_SECURE: "true", SESSION_COOKIE_SAME_SITE: "lax",
  });
});

test("environment example describes both public origins and cookie defaults", () => {
  const example = readFileSync(".env.example", "utf8");
  for (const entry of ["APP_PORT=3218", "API_PORT=3219", "HTTPS_PORT=3443", "API_HTTPS_PORT=3444", "PUBLIC_API_BASE_URL=http://localhost:3219", "CORS_ALLOWED_ORIGINS=http://localhost:3218,http://127.0.0.1:3218", "SESSION_COOKIE_SECURE=false", "SESSION_COOKIE_SAME_SITE=lax"]) {
    assert.ok(example.split(/\r?\n/).includes(entry), `missing ${entry}`);
  }
});
