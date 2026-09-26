import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import test from "node:test";

const dockerLanScript = readFileSync("scripts/docker-lan.mjs", "utf8");
const webRequire = createRequire(new URL("../web/package.json", import.meta.url));
const { load: parseYaml } = webRequire("js-yaml");
const ignore = webRequire("ignore");
const webDockerfile = readFileSync("web/Dockerfile", "utf8");
const dockerfile = readFileSync("backend/Dockerfile", "utf8");
const dockerCompose = readFileSync("docker-compose.yml", "utf8");
const compose = parseYaml(dockerCompose);
const rootEnvExample = readFileSync(".env.example", "utf8");
const backendEnvExample = readFileSync("backend/.env.example", "utf8");
const rootPackage = JSON.parse(readFileSync("package.json", "utf8"));
const instructions = (source) => source.replace(/\\\r?\n\s*/g, " ").split(/\r?\n/)
  .map((line) => line.trim()).filter((line) => line && !line.startsWith("#"));
const qualityWorkflow = readFileSync(
  ".github/workflows/quality-gates.yml",
  "utf8",
);
const deployWorkflow = readFileSync(
  ".github/workflows/deploy-local.yml",
  "utf8",
);
const parsedDeployWorkflow = parseYaml(deployWorkflow);
const deployStep = (name) => parsedDeployWorkflow.jobs.deploy.steps.find(
  (step) => step.name === name,
);

test("quality workflow installs and builds web independently", () => {
  assert.match(qualityWorkflow, /cache-dependency-path:\s+web\/package-lock\.json/);
  assert.match(qualityWorkflow, /npm ci --prefix web/);
  assert.match(qualityWorkflow, /npm run quality --prefix web/);
  assert.match(qualityWorkflow, /cd backend && go test \.\/\.\.\./);
  assert.match(qualityWorkflow, /docker compose build web backend worker/);
});

test("Windows deploy checks both services", () => {
  assert.match(deployWorkflow, /API_PORT:/);
  assert.match(deployWorkflow, /API_HTTPS_PORT:/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 web/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 backend/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 worker/);
  assert.match(deployWorkflow, /docker compose exec -T worker/);
  assert.match(deployWorkflow, /node scripts\/smoke-stack\.mjs/);
});

test("quality workflow runs explicit project and infrastructure gates", () => {
  assert.match(qualityWorkflow, /run:\s+npm run test:api-docs/);
  assert.match(qualityWorkflow, /run:\s+npm run test:infra/);
  assert.match(qualityWorkflow, /run:\s+npm run test:smoke/);
  assert.match(qualityWorkflow, /PUBLIC_API_BASE_URL:\s+http:\/\/localhost:3219/);
  assert.match(qualityWorkflow, /run:\s+npm run build --prefix web/);
  assert.doesNotMatch(qualityWorkflow, /run:\s+npm ci\s*$/m);
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
  assert.match(deployWorkflow, /workflow_dispatch:/);
  assert.match(deployWorkflow, /branches:\s*\n\s+- main\s*\n\s+- master/);
  assert.match(deployWorkflow, /shell:\s+powershell/);
  assert.match(
    deployWorkflow,
    /runs-on:\s*\[self-hosted,\s*windows,\s*daily-speaking\]/,
  );
});

test("local deploy workflow verifies quality, deploys the LAN Docker app, and checks health", () => {
  assert.match(deployWorkflow, /POSTGRES_PORT:\s+\$\{\{\s*vars\.POSTGRES_PORT/);
  assert.match(deployWorkflow, /HTTPS_PORT:\s+\$\{\{\s*vars\.HTTPS_PORT/);
  assert.match(deployWorkflow, /API_PORT:\s+\$\{\{\s*vars\.API_PORT\s*\|\|\s*'3219'/);
  assert.match(deployWorkflow, /API_HTTPS_PORT:\s+\$\{\{\s*vars\.API_HTTPS_PORT\s*\|\|\s*'3444'/);
  assert.match(deployWorkflow, /uses:\s+actions\/setup-go@v5/);
  assert.match(deployWorkflow, /go-version-file:\s+backend\/go\.mod/);
  assert.match(deployWorkflow, /cache-dependency-path:\s+web\/package-lock\.json/);
  assert.match(deployWorkflow, /run:\s+npm ci --prefix web/);
  assert.match(deployWorkflow, /run:\s+npm run quality/);
  assert.match(deployWorkflow, /\.\\scripts\\setup-lan-https-proxy\.ps1/);
  assert.match(deployWorkflow, /docker compose ps/);
  assert.match(deployWorkflow, /WEB_BASE_URL = "http:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:APP_PORT\}"/);
  assert.match(deployWorkflow, /API_BASE_URL = "http:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:API_PORT\}"/);
  assert.doesNotMatch(deployWorkflow, /ServerCertificateValidationCallback/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 web/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 backend/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 worker/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 lan-https/);
});

test("trusted HTTPS verification starts Node with the explicit mkcert root CA and HTTPS origins", () => {
  const run = deployStep("Verify trusted HTTPS endpoints")?.run;
  assert.equal(typeof run, "string");

  assert.match(run, /mkcert -CAROOT/);
  assert.match(run, /Join-Path[^\r\n]+["']rootCA\.pem["']/);
  assert.match(run, /Test-Path -LiteralPath \$mkcertRootCaPath -PathType Leaf/);
  assert.match(run, /\$env:NODE_EXTRA_CA_CERTS\s*=\s*\(Resolve-Path -LiteralPath \$mkcertRootCaPath\)\.Path/);
  assert.match(run, /\$env:WEB_BASE_URL\s*=\s*"https:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:HTTPS_PORT\}"/);
  assert.match(run, /\$env:API_BASE_URL\s*=\s*"https:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:API_HTTPS_PORT\}"/);
  assert.match(run, /node scripts\/smoke-stack\.mjs/);

  const caCheck = run.indexOf("Test-Path -LiteralPath $mkcertRootCaPath -PathType Leaf");
  const caExport = run.indexOf("$env:NODE_EXTRA_CA_CERTS");
  const webUrlExport = run.indexOf("$env:WEB_BASE_URL");
  const apiUrlExport = run.indexOf("$env:API_BASE_URL");
  const nodeStart = run.indexOf("node scripts/smoke-stack.mjs");
  assert.ok(caCheck >= 0 && caCheck < caExport, "the mkcert root must exist before it is exported");
  assert.ok(caExport < nodeStart, "the CA must be exported before Node starts");
  assert.ok(webUrlExport < nodeStart, "the web HTTPS origin must be exported before Node starts");
  assert.ok(apiUrlExport < nodeStart, "the API HTTPS origin must be exported before Node starts");
});

test("HTTP smoke verifies the canonical web redirect without following untrusted HTTPS", () => {
  const run = deployStep("Smoke separate HTTP services")?.run;
  assert.equal(typeof run, "string");

  assert.match(
    run,
    /\$env:EXPECTED_WEB_REDIRECT_BASE_URL\s*=\s*"https:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:HTTPS_PORT\}"/,
  );
  assert.match(run, /\$env:WEB_BASE_URL\s*=\s*"http:\/\/\$\{env:LAN_HOST_IP\}:\$\{env:APP_PORT\}"/);
  assert.doesNotMatch(run, /NODE_EXTRA_CA_CERTS/);
});

test("trusted HTTPS verification does not bypass trust or use the legacy PowerShell HTTP client", () => {
  const run = deployStep("Verify trusted HTTPS endpoints")?.run;
  assert.equal(typeof run, "string");

  assert.doesNotMatch(run, /NODE_TLS_REJECT_UNAUTHORIZED|SkipCertificateCheck|ServerCertificateValidationCallback|TrustAllCertsPolicy|curl(?:\.exe)?\s+[^\r\n]*-k\b/i);
  assert.doesNotMatch(run, /Invoke-RestMethod|Invoke-WebRequest|ServicePointManager|Invoke-TrustedHttpsWithRetry/);
});

test("local deploy uses a stable Docker Compose project name", () => {
  assert.match(deployWorkflow, /COMPOSE_PROJECT_NAME:\s+daily-speaking/);
  assert.match(dockerLanScript, /COMPOSE_PROJECT_NAME/);
  assert.match(dockerLanScript, /"--remove-orphans"/);
  const httpsSetup = readFileSync("scripts/setup-lan-https-proxy.ps1", "utf8");
  assert.match(httpsSetup, /docker compose up --build -d --remove-orphans web backend worker postgres\r?\n\s*if \(\$LASTEXITCODE -ne 0\) \{\r?\n\s*throw "Failed to build or start web, backend, worker, and postgres services\."/);
  assert.match(httpsSetup, /docker compose up -d --force-recreate --no-deps lan-https\r?\n\s*if \(\$LASTEXITCODE -ne 0\) \{\r?\n\s*throw "Failed to recreate lan-https service with current TLS configuration\."/);
  assert.doesNotMatch(httpsSetup, /docker compose down[^\r\n]*-v/);
  assert.match(rootPackage.scripts["docker:app"], /up --build -d --remove-orphans web backend worker postgres/);
});

test("local deploy workflow defaults to the Docker-local Python Whisper backend", () => {
  assert.match(deployWorkflow, /WHISPER_BACKEND:\s+openai/);
  assert.doesNotMatch(deployWorkflow, /vars\.WHISPER_BACKEND/);
  assert.match(deployWorkflow, /WHISPER_PYTHON_BIN:\s+\/opt\/whisper\/bin\/python/);
  assert.match(deployWorkflow, /WHISPER_OPENAI_MODEL_DIR:\s+\/app\/tools\/whisper\/openai-models/);
  assert.match(deployWorkflow, /WHISPER_OPENAI_CACHE_DIR:\s+\/app\/tools\/whisper\/cache/);
  assert.match(deployWorkflow, /WHISPER_FFMPEG_BIN:\s+\/usr\/bin\/ffmpeg/);
  assert.match(deployWorkflow, /WHISPER_OPENAI_DEVICE:\s+cpu/);
  assert.match(deployWorkflow, /WHISPER_OPENAI_FP16:\s+false/);
  assert.match(deployWorkflow, /WHISPER_OPENAI_MODEL:\s+base/);
  assert.match(deployWorkflow, /WHISPER_LANGUAGE:\s+auto/);
  assert.doesNotMatch(deployWorkflow, /vars\.WHISPER_OPENAI_MODEL/);
  assert.doesNotMatch(deployWorkflow, /vars\.WHISPER_LANGUAGE/);
});

test("multi-pass analysis concurrency is source-controlled for clean Windows deploys", () => {
  const envExample = readFileSync(".env.example", "utf8");

  assert.match(deployWorkflow, /AI_ANALYSIS_CONCURRENCY:\s+3/);
  assert.doesNotMatch(deployWorkflow, /vars\.AI_ANALYSIS_CONCURRENCY/);
  assert.match(
    dockerCompose,
    /AI_ANALYSIS_CONCURRENCY:\s+\$\{AI_ANALYSIS_CONCURRENCY:-3\}/,
  );
  assert.match(envExample, /AI_ANALYSIS_CONCURRENCY=3/);
});

test("local deploy workflow verifies Whisper inside the worker container", () => {
  assert.match(deployWorkflow, /name:\s+Verify Docker Whisper runtime/);
  assert.match(deployWorkflow, /docker compose exec -T worker sh -lc/);
  assert.match(deployWorkflow, /test -x "\$WHISPER_PYTHON_BIN"/);
  assert.match(deployWorkflow, /\$WHISPER_PYTHON_BIN -m whisper --help/);
  assert.match(deployWorkflow, /echo whisper-ok/);
});

test("local deploy workflow configures persistent uploaded media storage", () => {
  assert.match(deployWorkflow, /UPLOADS_DIR:\s+\/app\/uploads/);
  assert.match(deployWorkflow, /UPLOADS_HOST_DIR:\s+\$\{\{\s*vars\.UPLOADS_HOST_DIR/);
  assert.match(deployWorkflow, /D:\\DailySpeaking\\data\\uploads/);
  assert.equal(parsedDeployWorkflow.jobs.deploy.env.MEDIA_STORAGE_DRIVER, "local");
  assert.equal(parsedDeployWorkflow.jobs.deploy.env.MEDIA_S3_FORCE_PATH_STYLE, "${{ vars.MEDIA_S3_FORCE_PATH_STYLE || 'false' }}");
  assert.equal(parsedDeployWorkflow.jobs.deploy.env.MEDIA_UPLOAD_URL_TTL, "${{ vars.MEDIA_UPLOAD_URL_TTL || '15m' }}");
  assert.equal(parsedDeployWorkflow.jobs.deploy.env.MEDIA_MULTIPART_PART_SIZE_BYTES, "${{ vars.MEDIA_MULTIPART_PART_SIZE_BYTES || '8388608' }}");
});

test("media storage configuration remains local-compatible and server-only", () => {
  const { web, backend, worker } = compose.services;
  const expectedServerMedia = {
    MEDIA_STORAGE_DRIVER: "${MEDIA_STORAGE_DRIVER:-local}",
    MEDIA_S3_REGION: "${MEDIA_S3_REGION:-}",
    MEDIA_S3_BUCKET: "${MEDIA_S3_BUCKET:-}",
    MEDIA_S3_ENDPOINT: "${MEDIA_S3_ENDPOINT:-}",
    MEDIA_S3_FORCE_PATH_STYLE: "${MEDIA_S3_FORCE_PATH_STYLE:-false}",
    MEDIA_S3_ACCESS_KEY_ID: "${MEDIA_S3_ACCESS_KEY_ID:-}",
    MEDIA_S3_SECRET_ACCESS_KEY: "${MEDIA_S3_SECRET_ACCESS_KEY:-}",
    MEDIA_S3_SESSION_TOKEN: "${MEDIA_S3_SESSION_TOKEN:-}",
    MEDIA_UPLOAD_URL_TTL: "${MEDIA_UPLOAD_URL_TTL:-15m}",
    MEDIA_MULTIPART_PART_SIZE_BYTES: "${MEDIA_MULTIPART_PART_SIZE_BYTES:-8388608}",
  };
  for (const [name, value] of Object.entries(expectedServerMedia)) {
    assert.equal(backend.environment[name], value, `backend ${name}`);
    assert.equal(worker.environment[name], value, `worker ${name}`);
    assert.equal(web.environment[name], undefined, `web must not receive ${name}`);
  }
  assert.deepEqual(worker.volumes, backend.volumes);
  assert.ok(backend.volumes.includes("${UPLOADS_HOST_DIR:-./.data/uploads}:/app/uploads"));
});

test("Windows deploy keeps optional S3 credentials scoped to the Compose start step", () => {
  const deployEnv = parsedDeployWorkflow.jobs.deploy.env;
  const stepEnv = deployStep("Build and start local Docker HTTPS stack")?.env;
  for (const name of ["MEDIA_S3_ACCESS_KEY_ID", "MEDIA_S3_SECRET_ACCESS_KEY", "MEDIA_S3_SESSION_TOKEN"]) {
    assert.equal(deployEnv[name], undefined, `${name} must not be job-wide`);
    assert.equal(stepEnv[name], `\${{ secrets.${name} }}`);
  }
  assert.doesNotMatch(deployWorkflow, /IsNullOrWhiteSpace\(\$env:MEDIA_S3_/);
});

test("environment examples document the local default and optional S3 contract", () => {
  for (const example of [rootEnvExample, backendEnvExample]) {
    assert.match(example, /^MEDIA_STORAGE_DRIVER=local$/m);
    assert.match(example, /^MEDIA_S3_REGION=$/m);
    assert.match(example, /^MEDIA_S3_BUCKET=$/m);
    assert.match(example, /^MEDIA_S3_ENDPOINT=$/m);
    assert.match(example, /^MEDIA_S3_FORCE_PATH_STYLE=false$/m);
    assert.match(example, /^MEDIA_S3_ACCESS_KEY_ID=$/m);
    assert.match(example, /^MEDIA_S3_SECRET_ACCESS_KEY=$/m);
    assert.match(example, /^MEDIA_S3_SESSION_TOKEN=$/m);
    assert.match(example, /^MEDIA_UPLOAD_URL_TTL=15m$/m);
    assert.match(example, /^MEDIA_MULTIPART_PART_SIZE_BYTES=8388608$/m);
    assert.match(example, /^MEDIA_SWEEP_INTERVAL=15m$/m);
  }
});

test("local deploy passes Cartesia credentials from the correct GitHub stores", () => {
  assert.match(
    deployWorkflow,
    /CARTESIA_API_KEY:\s+\$\{\{\s*secrets\.CARTESIA_API_KEY\s*\}\}/,
  );
  assert.doesNotMatch(deployWorkflow, /vars\.CARTESIA_API_KEY/);
  assert.match(
    deployWorkflow,
    /CARTESIA_VOICE_ID:\s+\$\{\{\s*vars\.CARTESIA_VOICE_ID\s*\}\}/,
  );
});

test("mobile signing secret stays server-side and reaches only the backend", () => {
  assert.match(
    deployWorkflow,
    /AUTH_ACCESS_TOKEN_SECRET:\s+\$\{\{\s*secrets\.AUTH_ACCESS_TOKEN_SECRET\s*\}\}/,
  );
  assert.match(
    dockerCompose,
    /AUTH_ACCESS_TOKEN_SECRET:\s+\$\{AUTH_ACCESS_TOKEN_SECRET:-\}/,
  );
  assert.equal(compose.services.web.environment.AUTH_ACCESS_TOKEN_SECRET, undefined);
  assert.equal(compose.services.backend.environment.AUTH_ACCESS_TOKEN_SECRET, "${AUTH_ACCESS_TOKEN_SECRET:-}");
  assert.equal(deployStep("Build and start local Docker HTTPS stack")?.env.AUTH_ACCESS_TOKEN_SECRET, "${{ secrets.AUTH_ACCESS_TOKEN_SECRET }}");
  assert.equal(parsedDeployWorkflow.jobs.deploy.env.AUTH_ACCESS_TOKEN_SECRET, undefined);
});

test("local deploy stops before Docker when Cartesia configuration is missing", () => {
  assert.match(deployWorkflow, /name:\s+Validate Cartesia configuration/);
  assert.match(
    deployWorkflow,
    /IsNullOrWhiteSpace\(\$env:CARTESIA_API_KEY\)/,
  );
  assert.match(
    deployWorkflow,
    /IsNullOrWhiteSpace\(\$env:CARTESIA_VOICE_ID\)/,
  );
  assert.doesNotMatch(deployWorkflow, /Write-Host[^\n]*CARTESIA_API_KEY/);
});

test("local deploy verifies Cartesia variables reached the worker container without printing them", () => {
  assert.match(deployWorkflow, /name:\s+Verify Docker Cartesia configuration/);
  assert.match(
    deployWorkflow,
    /test -n "\$CARTESIA_API_KEY" && test -n "\$CARTESIA_VOICE_ID" && echo cartesia-config-ok/,
  );
  assert.doesNotMatch(deployWorkflow, /env \| grep CARTESIA/);
});

test("repository forces LF endings for scripts used inside Linux containers", () => {
  const gitAttributes = readFileSync(".gitattributes", "utf8");

  assert.match(gitAttributes, /\*\.sh\s+text\s+eol=lf/);
  assert.match(gitAttributes, /Dockerfile\s+text\s+eol=lf/);
});

test("repository keeps the generated OpenAPI document on LF checkouts", () => {
  const attributes = execFileSync(
    "git",
    ["check-attr", "eol", "--", "backend/docs/openapi.json"],
    { encoding: "utf8" },
  );

  assert.equal(attributes.trim(), "backend/docs/openapi.json: eol: lf");
});

test("Docker build creates public before copying it into the runtime image", () => {
  assert.match(webDockerfile, /RUN mkdir -p public && npm run build/);
  assert.match(webDockerfile, /COPY --from=build \/app\/public \.\/public/);
});

test("Docker runtime includes the local Python Whisper backend", () => {
  assert.match(dockerfile, /FROM debian:bookworm-slim AS runtime/);
  assert.match(dockerfile, /python3-venv/);
  assert.match(dockerfile, /ffmpeg/);
  assert.match(dockerfile, /openai-whisper/);
  assert.match(dockerfile, /WHISPER_BACKEND=openai/);
  assert.match(dockerfile, /WHISPER_PYTHON_BIN=\/opt\/whisper\/bin\/python/);
  assert.match(dockerfile, /WHISPER_OPENAI_MODEL=base(?:\s|$)/);
  assert.match(dockerfile, /WHISPER_FFMPEG_BIN=\/usr\/bin\/ffmpeg/);
});

test("Compose gives independent contexts and bounded worker execution", () => {
  assert.deepEqual(Object.keys(compose.services).sort(), ["backend", "lan-https", "postgres", "web", "worker"]);
  const { web, backend, worker } = compose.services;
  assert.equal(web.build.context, "./web");
  assert.equal(backend.build.context, "./backend");
  assert.deepEqual(web.ports, ["0.0.0.0:${APP_PORT:-3218}:3000"]);
  assert.deepEqual(backend.ports, ["0.0.0.0:${API_PORT:-3219}:3000"]);
  assert.equal(web.depends_on, undefined);
  assert.deepEqual(backend.depends_on, { postgres: { condition: "service_healthy" } });
  assert.equal(worker.build.context, "./backend");
  assert.deepEqual(worker.command, ["./daily-speaking-worker"]);
  assert.deepEqual(worker.depends_on, { postgres: { condition: "service_healthy" } });
  assert.equal(worker.ports, undefined);
  assert.equal(worker.environment.WORKER_RECORDING_CONCURRENCY, "${WORKER_RECORDING_CONCURRENCY:-1}");
  assert.equal(worker.environment.WORKER_SHADOWING_CONCURRENCY, "${WORKER_SHADOWING_CONCURRENCY:-2}");
  assert.deepEqual(web.environment, {
    PUBLIC_WEB_BASE_URL: "${PUBLIC_WEB_BASE_URL:-}",
    PUBLIC_API_BASE_URL: "${PUBLIC_API_BASE_URL:-http://localhost:3219}",
  });
  assert.match(web.healthcheck.test.join(" "), /http:\/\/127\.0\.0\.1:3000\/web-healthz/);
  assert.doesNotMatch(web.healthcheck.test.join(" "), /backend|3219|PUBLIC_API/);
  assert.match(backend.healthcheck.test.join(" "), /http:\/\/127\.0\.0\.1:3000\/healthz/);
  assert.deepEqual(compose.services["lan-https"].depends_on, ["web", "backend"]);
  assert.equal(existsSync("Dockerfile"), false);
  assert.equal(existsSync("docker-entrypoint.sh"), false);
});

test("Compose keeps backend credentials and persistent data with their owners", () => {
  const { web, backend, worker, postgres } = compose.services;
  assert.ok(backend, "backend service is required");
  assert.equal(web.volumes, undefined);
  assert.deepEqual(backend.volumes, [
    "${WHISPER_TOOLS_HOST_DIR:-./backend/tools}:/app/tools",
    "${UPLOADS_HOST_DIR:-./.data/uploads}:/app/uploads",
  ]);
  assert.deepEqual(worker.volumes, backend.volumes);
  assert.deepEqual(postgres.volumes, ["postgres_data:/var/lib/postgresql/data"]);
  assert.deepEqual(postgres.ports, ["127.0.0.1:${POSTGRES_PORT:-5432}:5432"]);
  assert.ok(Object.hasOwn(compose.volumes, "postgres_data"));
  for (const [key, value] of Object.entries({
    DATABASE_URL: "postgres://postgres:postgres@postgres:5432/daily_speaking",
    CORS_ALLOWED_ORIGINS: "${CORS_ALLOWED_ORIGINS:-http://localhost:3218,http://localhost:3219}",
    SESSION_COOKIE_SECURE: "${SESSION_COOKIE_SECURE:-false}",
    SESSION_COOKIE_SAME_SITE: "${SESSION_COOKIE_SAME_SITE:-lax}",
    SESSION_COOKIE_DOMAIN: "${SESSION_COOKIE_DOMAIN:-}",
    CARTESIA_API_KEY: "${CARTESIA_API_KEY:-}",
    CARTESIA_VOICE_ID: "${CARTESIA_VOICE_ID:-}",
  })) assert.equal(backend.environment[key], value, key);
});

test("application images ship only their own production runtime and assets", () => {
  const web = instructions(webDockerfile);
  const backend = instructions(dockerfile);
  assert.deepEqual(web.filter((line) => line.startsWith("FROM ")), [
    "FROM node:22-alpine AS deps", "FROM deps AS build", "FROM node:22-alpine AS runtime",
  ]);
  assert.ok(web.includes("COPY --from=build /app/.next/standalone ./"));
  assert.ok(web.includes("COPY --from=build /app/.next/static ./.next/static"));
  assert.ok(web.includes("EXPOSE 3000"));
  assert.ok(web.includes('CMD ["node", "server.js"]'));
  assert.doesNotMatch(web.join("\n"), /golang|daily-speaking-api|whisper|ffmpeg|backend|PUBLIC_API_BASE_URL/);
  assert.deepEqual(backend.filter((line) => line.startsWith("FROM ")), [
    "FROM golang:1.26.2-alpine AS build", "FROM debian:bookworm-slim AS runtime",
  ]);
  assert.ok(backend.includes("RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-api ./cmd/api"));
  assert.ok(backend.includes("RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-worker ./cmd/worker"));
  assert.ok(backend.includes("COPY --from=build /out/daily-speaking-api ./daily-speaking-api"));
  assert.ok(backend.includes("COPY --from=build /out/daily-speaking-worker ./daily-speaking-worker"));
  assert.ok(backend.includes("ENV APP_ADDR=:3000"));
  assert.ok(backend.includes("EXPOSE 3000"));
  assert.ok(backend.includes('CMD ["./daily-speaking-api"]'));
  assert.match(backend.join("\n"), /apt-get install[^\n]*ca-certificates curl ffmpeg python3 python3-venv/);
  assert.doesNotMatch(backend.join("\n"), /node:|next-build|server\.js|COPY.*web/);
});

test("project build contexts exclude secrets and generated files but retain source assets", () => {
  for (const project of ["web", "backend"]) {
    const excluded = ignore().add(readFileSync(`${project}/.dockerignore`, "utf8"));
    for (const file of [".env", ".env.local", ".git/config", "node_modules/pkg/index.js", "coverage/report.json", "tmp/output", ".cache/data"]) {
      assert.equal(excluded.ignores(file), true, `${project}: must exclude ${file}`);
    }
    for (const file of project === "web"
      ? ["app/layout.tsx", "src/lib/apiConfig.ts", "package-lock.json", "public/logo.svg"]
      : ["go.mod", "go.sum", "cmd/api/main.go", "migrations/0001_init.sql", "docs/openapi.json", "tools/whisper/cache/.gitkeep"]) {
      assert.equal(excluded.ignores(file), false, `${project}: must retain ${file}`);
    }
    for (const file of project === "web"
      ? [".next/server/app.js", "tsconfig.tsbuildinfo", "out/index.html"]
      : [".venv/bin/python", "daily-speaking-api", "uploads/recording.webm", "tools/whisper/openai-models/base.pt"]) {
      assert.equal(excluded.ignores(file), true, `${project}: must exclude ${file}`);
    }
  }
});

test("web health responds without API configuration or external requests", async () => {
  const route = "web/app/web-healthz/route.ts";
  assert.equal(existsSync(route), true, "web-only health route is required");
  const { transpileModule, ModuleKind } = webRequire("typescript");
  const { outputText } = transpileModule(readFileSync(route, "utf8"), { compilerOptions: { module: ModuleKind.ESNext } });
  const { GET } = await import(`data:text/javascript,${encodeURIComponent(outputText)}`);
  const response = await GET();
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { ok: true, service: "web" });
});

test("Docker runtime and Compose use persistent uploaded media storage", () => {
  assert.match(dockerfile, /UPLOADS_DIR=\/app\/uploads/);
  assert.match(dockerfile, /mkdir -p \/app\/uploads/);
  assert.match(dockerCompose, /UPLOADS_DIR:\s+\$\{UPLOADS_DIR:-\/app\/uploads\}/);
  assert.match(dockerCompose, /\$\{UPLOADS_HOST_DIR:-\.\/\.data\/uploads\}:\/app\/uploads/);
  assert.doesNotMatch(dockerCompose, /\.\/public\/uploads:\/app\/public\/uploads/);
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
  assert.match(
    dockerCompose,
    /WHISPER_OPENAI_MODEL:\s+\$\{WHISPER_OPENAI_MODEL:-base\}/,
  );
  assert.match(
    dockerCompose,
    /WHISPER_LANGUAGE:\s+\$\{WHISPER_LANGUAGE:-auto\}/,
  );
});
