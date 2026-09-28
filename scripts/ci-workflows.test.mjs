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
const apiSmoke = readFileSync("scripts/smoke-api.mjs", "utf8");
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
const cartesiaRealtimePreflight = readFileSync(
  "scripts/preflight-cartesia-realtime-migration.ps1",
  "utf8",
);
const httpsSetup = readFileSync("scripts/setup-lan-https-proxy.ps1", "utf8");
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
  assert.match(apiSmoke, /SMOKE_AUTH_ACCESS_TOKEN_SECRET/);
  assert.match(apiSmoke, /AUTH_ACCESS_TOKEN_SECRET:\s*localIdentitySecret/);
  assert.doesNotMatch(qualityWorkflow, /AUTH_ACCESS_TOKEN_SECRET/);
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

test("Windows rollout blocks replacement until legacy interview finalization is drained", () => {
  const preflight = deployStep("Preflight Cartesia realtime migration");
  const build = deployStep("Build and start local Docker HTTPS stack");
  const recovery = deployStep("Restore quiesced API after interrupted rollout");
  assert.equal(parsedDeployWorkflow.concurrency["cancel-in-progress"], false,
    "a newer push must wait instead of cancelling a rollout after it quiesces the API");
  assert.equal(preflight?.run, ".\\scripts\\preflight-cartesia-realtime-migration.ps1");
  assert.ok(parsedDeployWorkflow.jobs.deploy.steps.indexOf(preflight) < parsedDeployWorkflow.jobs.deploy.steps.indexOf(build));

  for (const fragment of [
    "0014_cartesia_realtime_transcripts.sql",
    "queued", "running", "retry_wait",
    "recording.process", "guest.preview",
    "session.recording_id = job.resource_id",
    "session.guest_preview_id = job.resource_id",
  ]) assert.match(cartesiaRealtimePreflight, new RegExp(fragment.replaceAll(".", "\\.")));
  assert.match(cartesiaRealtimePreflight, /schema_migrations WHERE name = '\$MigrationName'/);
  assert.match(cartesiaRealtimePreflight, /string_agg\(active\.kind \|\| '\/' \|\| active\.state/);
  assert.match(cartesiaRealtimePreflight, /if \(-not \$QuiesceBackend\) \{[\s\S]*?return[\s\S]*?\}/);
  assert.doesNotMatch(cartesiaRealtimePreflight, /docker (?:stop|rm)[^\r\n]*worker/i);
  assert.doesNotMatch(cartesiaRealtimePreflight, /DATABASE_URL|CARTESIA_API_KEY|SELECT\s+(?:job|j)\.(?:id|resource_id)/i);

  const gateCall = "Assert-NoActiveLegacyInterviewJobs -PostgresContainerId";
  const firstGate = cartesiaRealtimePreflight.indexOf(gateCall);
  const backendStop = cartesiaRealtimePreflight.indexOf("docker stop --time 30");
  const finalGate = cartesiaRealtimePreflight.indexOf(gateCall, firstGate + 1);
  assert.ok(firstGate >= 0 && firstGate < backendStop && backendStop < finalGate,
    "the old API must be quiesced between two checks while the old worker stays running");

  const imageBuild = httpsSetup.indexOf("docker compose build web backend worker");
  const quiesce = httpsSetup.indexOf("preflight-cartesia-realtime-migration.ps1");
  const replace = httpsSetup.indexOf("docker compose up --no-build -d --remove-orphans web backend worker postgres");
  assert.ok(imageBuild >= 0 && imageBuild < quiesce && quiesce < replace,
    "images must build before the final quiesce and containers may be replaced only after it passes");

  assert.equal(recovery?.if, "${{ failure() || cancelled() }}");
  assert.match(recovery?.run ?? "", /DAILY_SPEAKING_QUIESCED_BACKEND_ID/);
  assert.match(recovery?.run ?? "", /docker start \$containerId/);
  assert.doesNotMatch(recovery?.run ?? "", /DATABASE_URL|CARTESIA_API_KEY|AUTH_ACCESS_TOKEN_SECRET/);
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
  assert.match(httpsSetup, /docker compose build web backend worker\r?\n\s*if \(\$LASTEXITCODE -ne 0\) \{\r?\n\s*throw "Failed to build web, backend, and worker services\. The existing stack was not replaced\."/);
  assert.match(httpsSetup, /docker compose up --no-build -d --remove-orphans web backend worker postgres\r?\n\s*if \(\$LASTEXITCODE -ne 0\) \{\r?\n\s*throw "Failed to start web, backend, worker, and postgres services\."/);
  assert.match(httpsSetup, /docker compose up -d --force-recreate --no-deps lan-https\r?\n\s*if \(\$LASTEXITCODE -ne 0\) \{\r?\n\s*throw "Failed to recreate lan-https service with current TLS configuration\."/);
  assert.doesNotMatch(httpsSetup, /docker compose down[^\r\n]*-v/);
  assert.match(rootPackage.scripts["docker:app"], /up --build -d --remove-orphans web backend worker postgres/);
});

test("local deploy uses Cartesia speech services with one secret key", () => {
  assert.match(deployWorkflow, /TRANSCRIPTION_LANGUAGE:\s+en/);
  assert.match(deployWorkflow, /FFMPEG_BINARY_PATH:\s+\/usr\/bin\/ffmpeg/);
  assert.match(deployWorkflow, /CARTESIA_API_KEY:\s+\$\{\{ secrets\.CARTESIA_API_KEY \}\}/);
  assert.doesNotMatch(deployWorkflow, /vars\.CARTESIA_API_KEY/);
  assert.match(deployWorkflow, /name:\s+Validate Cartesia configuration/);
  assert.equal(deployStep("Build and start local Docker HTTPS stack")?.env?.CARTESIA_API_KEY, "${{ secrets.CARTESIA_API_KEY }}");
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

test("local deploy workflow verifies Cartesia without uploading audio", () => {
  assert.match(deployWorkflow, /name:\s+Verify Docker Cartesia configuration/);
  assert.match(deployWorkflow, /docker compose exec -T worker \.\/daily-speaking-worker --check-cartesia/);
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
    assert.match(example, /^GUEST_PREVIEW_QUEUE_CAPACITY=100$/m);
    assert.match(example, /^WORKER_GUEST_PREVIEW_CONCURRENCY=1$/m);
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

test("identity signing secret stays server-side and reaches only the backend", () => {
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

test("local deploy rejects a missing or weak identity signing secret before Docker starts", () => {
  const validation = deployStep("Validate identity configuration");
  assert.equal(
    validation?.env?.AUTH_ACCESS_TOKEN_SECRET,
    "${{ secrets.AUTH_ACCESS_TOKEN_SECRET }}",
  );
  assert.match(validation?.run ?? "", /IsNullOrWhiteSpace\(\$env:AUTH_ACCESS_TOKEN_SECRET\)/);
  assert.match(validation?.run ?? "", /\$signingSecret = \$env:AUTH_ACCESS_TOKEN_SECRET\.Trim\(\)/);
  assert.match(validation?.run ?? "", /\$signingSecret\.Length -lt 32/);
  assert.match(validation?.run ?? "", /Settings > Secrets and variables > Actions > Secrets/);
  assert.doesNotMatch(validation?.run ?? "", /Write-(?:Host|Output)[^\r\n]*AUTH_ACCESS_TOKEN_SECRET/);

  const validationIndex = parsedDeployWorkflow.jobs.deploy.steps.indexOf(validation);
  const dockerIndex = parsedDeployWorkflow.jobs.deploy.steps.indexOf(
    deployStep("Build and start local Docker HTTPS stack"),
  );
  assert.ok(validationIndex >= 0 && validationIndex < dockerIndex);
});

test("local deploy verifies identity inside Docker before API smoke checks", () => {
  const verification = deployStep("Verify Docker identity configuration");
  const run = verification?.run ?? "";
  assert.match(run, /docker compose exec -T backend sh -lc/);
  assert.match(run, /test "\$\{#AUTH_ACCESS_TOKEN_SECRET\}" -ge 32/);
  assert.match(run, /echo identity-config-ok/);
  assert.doesNotMatch(run, /(?:echo|printf|env\s*\|)[^\r\n]*\$AUTH_ACCESS_TOKEN_SECRET/);

  const verificationIndex = parsedDeployWorkflow.jobs.deploy.steps.indexOf(verification);
  const smokeIndex = parsedDeployWorkflow.jobs.deploy.steps.indexOf(
    deployStep("Smoke separate HTTP services"),
  );
  assert.ok(verificationIndex >= 0 && verificationIndex < smokeIndex);
});

test("backend operations controls reach Windows deploy without leaking to web or worker", () => {
  const deployEnv = parsedDeployWorkflow.jobs.deploy.env;
  const stepEnv = deployStep("Build and start local Docker HTTPS stack")?.env;
  assert.equal(deployEnv.TRUSTED_PROXY_CIDRS, "${{ vars.TRUSTED_PROXY_CIDRS || '172.16.0.0/12' }}");
  assert.equal(deployEnv.RATE_LIMIT_ENABLED, "${{ vars.RATE_LIMIT_ENABLED || 'true' }}");
  assert.equal(deployEnv.READINESS_MAX_QUEUE_DEPTH, "${{ vars.READINESS_MAX_QUEUE_DEPTH || '1000' }}");
  assert.equal(deployEnv.WORKER_JOB_RETENTION, "${{ vars.WORKER_JOB_RETENTION || '720h' }}");
  assert.equal(stepEnv.METRICS_BEARER_TOKEN, "${{ secrets.METRICS_BEARER_TOKEN }}");
  assert.equal(deployEnv.METRICS_BEARER_TOKEN, undefined);
  assert.equal(compose.services.backend.environment.METRICS_BEARER_TOKEN, "${METRICS_BEARER_TOKEN:-}");
  assert.equal(compose.services.web.environment.METRICS_BEARER_TOKEN, undefined);
  assert.equal(compose.services.worker.environment.METRICS_BEARER_TOKEN, undefined);
  for (const example of [rootEnvExample, backendEnvExample]) {
    assert.match(example, /^RATE_LIMIT_ENABLED=true$/m);
    assert.match(example, /^READINESS_MAX_QUEUE_DEPTH=1000$/m);
    assert.match(example, /^METRICS_BEARER_TOKEN=$/m);
  }
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

test("local deploy verifies Cartesia connectivity without printing credentials or sending audio", () => {
  assert.match(deployWorkflow, /name:\s+Verify Docker Cartesia configuration/);
  assert.match(
    deployWorkflow,
    /docker compose exec -T worker \.\/daily-speaking-worker --check-cartesia/,
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

test("Docker runtime includes audio tools without a local speech model", () => {
  assert.match(dockerfile, /FROM debian:bookworm-slim AS runtime/);
  assert.match(dockerfile, /ffmpeg/);
  assert.doesNotMatch(dockerfile, /python3-venv|pip install/);
  assert.match(dockerfile, /FFMPEG_BINARY_PATH=\/usr\/bin\/ffmpeg/);
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
  assert.equal(worker.environment.WORKER_GUEST_PREVIEW_CONCURRENCY, "${WORKER_GUEST_PREVIEW_CONCURRENCY:-1}");
  assert.equal(worker.environment.WORKER_SHADOWING_CONCURRENCY, "${WORKER_SHADOWING_CONCURRENCY:-2}");
  assert.equal(backend.environment.GUEST_PREVIEW_QUEUE_CAPACITY, "${GUEST_PREVIEW_QUEUE_CAPACITY:-100}");
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
  assert.deepEqual(backend.volumes, ["${UPLOADS_HOST_DIR:-./.data/uploads}:/app/uploads"]);
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
  assert.doesNotMatch(web.join("\n"), /golang|daily-speaking-api|ffmpeg|backend|PUBLIC_API_BASE_URL/);
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
  assert.match(backend.join("\n"), /apt-get install[^\n]*ca-certificates curl ffmpeg/);
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
      : ["go.mod", "go.sum", "cmd/api/main.go", "migrations/0001_init.sql", "docs/openapi.json"]) {
      assert.equal(excluded.ignores(file), false, `${project}: must retain ${file}`);
    }
    for (const file of project === "web"
      ? [".next/server/app.js", "tsconfig.tsbuildinfo", "out/index.html"]
      : [".venv/bin/python", "daily-speaking-api", "uploads/recording.webm", "tools/model-cache/base.pt"]) {
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

test("Docker Compose gives one Cartesia key to the API and worker", () => {
  assert.match(dockerCompose, /CARTESIA_API_KEY:\s+\$\{CARTESIA_API_KEY:-\}/);
  assert.match(dockerCompose, /FFMPEG_BINARY_PATH:\s+\$\{FFMPEG_BINARY_PATH:-\/usr\/bin\/ffmpeg\}/);
  assert.equal(compose.services.backend.environment.CARTESIA_API_KEY, "${CARTESIA_API_KEY:-}");
  assert.equal(compose.services.worker.environment.CARTESIA_API_KEY, "${CARTESIA_API_KEY:-}");
});
