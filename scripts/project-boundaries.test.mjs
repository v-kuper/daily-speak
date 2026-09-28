import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import test from "node:test";

test("web and backend are standalone projects", () => {
  for (const path of [
    "web/package.json",
    "web/package-lock.json",
    "web/app/layout.tsx",
    "web/src/store/index.ts",
    "web/Dockerfile",
    "backend/go.mod",
    "backend/Dockerfile",
  ]) {
    assert.equal(existsSync(path), true, `missing ${path}`);
  }
});

test("root package only orchestrates independent projects", () => {
  const rootPackage = JSON.parse(readFileSync("package.json", "utf8"));
  assert.deepEqual(rootPackage.dependencies ?? {}, {});
  assert.deepEqual(rootPackage.devDependencies ?? {}, {});
  assert.match(rootPackage.scripts.quality, /web:quality/);
  assert.match(rootPackage.scripts.quality, /backend:test/);
});

test("repository keeps one canonical documentation set and ignores local tooling state", () => {
  for (const path of [
    "docs/README.md",
    "docs/ARCHITECTURE.md",
    "docs/BACKEND_DEVELOPMENT.md",
    "docs/api-compatibility.md",
    "docs/BACKEND_OPERATIONS.md",
    "docs/LOCAL_WINDOWS_CICD.md",
    "docs/TECH_DEBT.md",
  ]) {
    assert.equal(existsSync(path), true, `missing canonical document ${path}`);
  }
  for (const path of [
    "docs/EA_AGENT_DESIGN_BRIEF.md",
    "docs/FUNCTIONAL_REQUIREMENTS_DESIGN.md",
    "docs/superpowers/plans/2026-09-21-web-backend-separation.md",
    "web/Daily Speaking Practice.html",
    "web/design-qa.md",
  ]) {
    assert.equal(existsSync(path), false, `obsolete artifact returned: ${path}`);
  }

  const documentationIndex = readFileSync("docs/README.md", "utf8");
  const agentInstructions = readFileSync("AGENTS.md", "utf8");
  assert.match(documentationIndex, /ARCHITECTURE\.md/);
  assert.match(documentationIndex, /BACKEND_DEVELOPMENT\.md/);
  assert.match(agentInstructions, /docs\/BACKEND_DEVELOPMENT\.md/);
  assert.match(documentationIndex, /Temporary implementation[\s\S]*do not belong/);
  for (const path of [".idea/workspace.xml", ".ai/mcp/mcp.json"]) {
    const result = spawnSync("git", ["check-ignore", "--quiet", "--no-index", path]);
    assert.equal(result.status, 0, `${path} must remain local-only`);
  }
});

test("frontend source is not left at repository root", () => {
  assert.equal(existsSync("app"), false);
  assert.equal(existsSync("src"), false);
  assert.equal(existsSync("next.config.ts"), false);
});

test("web build traces are rooted in the standalone web project", () => {
  const config = readFileSync("web/next.config.ts", "utf8");
  assert.match(config, /outputFileTracingRoot:\s*projectRoot/);
  assert.match(config, /fileURLToPath\(import\.meta\.url\)/);
});

test("standalone backend upload defaults are ignored by Git", () => {
  for (const path of [
    "backend/public/uploads/recordings/user/recording.webm",
    "backend/public/uploads/shadowing/user/pronunciation.mp3",
  ]) {
    const result = spawnSync("git", ["check-ignore", "--quiet", "--no-index", path]);
    assert.equal(result.status, 0, `${path} must be ignored by the repository Git configuration`);
  }
});

test("backend environment example documents server-only Cartesia speech services", () => {
  const env = Object.fromEntries(readFileSync("backend/.env.example", "utf8")
    .split(/\r?\n/)
    .filter((line) => line && !line.startsWith("#") && line.includes("="))
    .map((line) => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1).replace(/^"|"$/g, "")];
    }));
  assert.equal(env.CARTESIA_API_KEY, "");
  assert.equal(env.CARTESIA_API_VERSION, "2026-08-14");
  assert.equal(env.TRANSCRIPTION_LANGUAGE, "en");
});

test("backend source has no Next.js upstream", () => {
  const main = readFileSync("backend/cmd/api/main.go", "utf8");
  const server = readFileSync("backend/internal/httpapi/server.go", "utf8");
  const smoke = readFileSync("scripts/smoke-api.mjs", "utf8");
  assert.doesNotMatch(main, /NEXT_UPSTREAM_URL|NextURL|proxying Next/);
  assert.doesNotMatch(server, /httputil|NewSingleHostReverseProxy|nextProxy|NextURL/);
  assert.doesNotMatch(smoke, /NEXT_|next/i);
  assert.match(smoke, /cwd:\s*"backend"/);
});

test("API and durable worker have separate process entrypoints", () => {
  const apiMain = readFileSync("backend/cmd/api/main.go", "utf8");
  const workerMain = readFileSync("backend/cmd/worker/main.go", "utf8");
  const runtime = readFileSync("backend/internal/worker/runtime.go", "utf8");
  const mediaCleanup = readFileSync("backend/internal/media/cleanup.go", "utf8");
  const mediaMaterializer = readFileSync("backend/internal/media/materializer.go", "utf8");
  const backgroundRuntime = readFileSync("backend/internal/background/runtime.go", "utf8");

  assert.doesNotMatch(apiMain, /RunWorkers|StartBackgroundWorkers/);
  assert.match(workerMain, /app\.NewWorker\(app\.WorkerConfig/);
  assert.match(workerMain, /runtime\.Run/);
  assert.doesNotMatch(workerMain, /internal\/httpapi|httpapi\./);
  assert.match(workerMain, /worker\.ConfigFromEnv/);
  assert.match(runtime, /workqueue\.Run/);
  assert.match(backgroundRuntime, /RecordingProcessor|GuestPreviewProcessor|ShadowingProcessor/);
  assert.match(mediaCleanup, /AbortExpiredUploads|EnqueueExpiredAssets|FinalizeFailure/);
  assert.equal(existsSync("backend/internal/httpapi/durable_workers.go"), false);
  assert.equal(existsSync("backend/internal/httpapi/recording_processing.go"), false);
  assert.match(mediaMaterializer, /verified_checksum_sha256|io\.LimitReader/);
  assert.equal(existsSync("backend/internal/httpapi/media_workers.go"), false);
  assert.equal(existsSync("backend/internal/httpapi/guest_preview_probe.go"), false);
  assert.match(readFileSync("backend/internal/media/probe.go", "utf8"), /func ProbeAudioDuration/);
  assert.equal(existsSync("backend/internal/storage/legacy.go"), false);
});

test("practice generation is an application service outside HTTP transport", () => {
  for (const path of [
    "backend/internal/practice/service.go",
    "backend/internal/practice/daily_questions.go",
    "backend/internal/practice/prompts.go",
    "backend/internal/practice/parsers.go",
    "backend/internal/practice/ollamaadapter/provider.go",
    "backend/internal/httpapi/practice_handlers.go",
  ]) {
    assert.equal(existsSync(path), true, `missing ${path}`);
  }

  const core = [
    "service.go",
    "daily_questions.go",
    "topic_guidance.go",
    "study_pack.go",
    "prompts.go",
    "parsers.go",
    "normalization.go",
  ].map((name) => readFileSync(`backend/internal/practice/${name}`, "utf8")).join("\n");
  const service = readFileSync("backend/internal/practice/service.go", "utf8");
  const provider = readFileSync("backend/internal/practice/ollamaadapter/provider.go", "utf8");
  const handler = readFileSync("backend/internal/httpapi/practice_handlers.go", "utf8");
  assert.doesNotMatch(core, /net\/http|internal\/httpapi|internal\/ai"/);
  assert.match(service, /CompletionProvider/);
  assert.match(provider, /ai\.ChatClient/);
  assert.doesNotMatch(provider, /ai\.PostChat/);
  assert.match(handler, /internal\/practice/);
  assert.doesNotMatch(handler, /PostChat|You generate|output format exactly/);
  assert.equal(existsSync("backend/internal/httpapi/ai_handlers.go"), false);
});

test("recording analysis owns its policy outside HTTP and provider adapters", () => {
  const coreFiles = [
    "model.go",
    "references.go",
    "analysis_types.go",
    "analysis_prompts.go",
    "analysis_review.go",
    "analysis_support.go",
    "analysis_service.go",
	"rewrite.go",
	"processing.go",
	"preview.go",
  ];
  for (const path of [
    ...coreFiles.map((name) => `backend/internal/recording/${name}`),
    "backend/internal/recording/ollamaadapter/provider.go",
    "backend/internal/httpapi/recording_analysis.go",
	"backend/internal/httpapi/recording_rewrite.go",
  ]) {
    assert.equal(existsSync(path), true, `missing ${path}`);
  }

  const core = coreFiles
    .map((name) => readFileSync(`backend/internal/recording/${name}`, "utf8"))
    .join("\n");
  const provider = readFileSync("backend/internal/recording/ollamaadapter/provider.go", "utf8");
  const transport = readFileSync("backend/internal/httpapi/recording_analysis.go", "utf8");
	const rewriteTransport = readFileSync("backend/internal/httpapi/recording_rewrite.go", "utf8");
  assert.doesNotMatch(core, /net\/http|internal\/httpapi|internal\/ai"/);
  assert.match(core, /AnalysisProvider/);
  assert.match(provider, /ai\.ChatClient/);
  assert.match(transport, /recording\.AnalysisInput/);
  assert.doesNotMatch(transport, /PostChat|error detector|adjudicator/);
	assert.match(rewriteTransport, /recording\.RewriteInput/);
	assert.doesNotMatch(rewriteTransport, /PostChat|natural conversational English/);
  const processingRepository = readFileSync("backend/internal/recording/processing_repository.go", "utf8");
  assert.equal(existsSync("backend/internal/httpapi/recording_processing.go"), false);
  assert.match(processingRepository, /LoadProcessingWork|SaveTranscript|CompleteRecording/);
  for (const name of [
    "recording_analysis_coordinator.go",
    "recording_analysis_prompts.go",
    "recording_analysis_review.go",
    "recording_analysis_types.go",
  ]) {
    assert.equal(existsSync(`backend/internal/httpapi/${name}`), false, `${name} returned to transport`);
  }
});

test("mobile recording creation keeps transport, policy, and SQL separate", () => {
  const transport = readFileSync("backend/internal/httpapi/recordings_create_v1.go", "utf8");
  const service = readFileSync("backend/internal/recording/create.go", "utf8");
  const repository = readFileSync("backend/internal/recording/create_repository.go", "utf8");

  assert.match(transport, /recordingCreator\.Create/);
  assert.doesNotMatch(transport, /SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue/);
  assert.match(service, /CreateUnitOfWork|validateCreateQuota|NormalizeCreateInput/);
  assert.doesNotMatch(service, /SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue|net\/http/);
  assert.match(repository, /SELECT |INSERT INTO|UPDATE |workqueue\.Enqueue/);
  assert.doesNotMatch(repository, /http\.Status|writeJSON|writeV1Error/);
});

test("recording deletion keeps cleanup policy outside HTTP and SQL adapters", () => {
  const transport = readFileSync("backend/internal/httpapi/recording_deletion.go", "utf8");
  const service = readFileSync("backend/internal/recording/delete.go", "utf8");
  const repository = readFileSync("backend/internal/recording/delete_repository.go", "utf8");

  assert.match(transport, /recordingDeleter\.Delete/);
  assert.doesNotMatch(transport, /SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue/);
  assert.match(service, /DeletionUnitOfWork|uniqueDeletionValues|QueueAsset/);
  assert.doesNotMatch(service, /SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue|net\/http/);
  assert.match(repository, /media_assets|processing_jobs|workqueue\.Enqueue/);
  assert.doesNotMatch(repository, /pending_file_deletions|feed_/);
  assert.doesNotMatch(repository, /http\.Status|writeJSON/);
});

test("recording query and retry keep persistence and queue mechanics outside HTTP", () => {
  const transport = readFileSync("backend/internal/httpapi/recordings_v1.go", "utf8");
  const queryService = readFileSync("backend/internal/recording/query.go", "utf8");
  const retryService = readFileSync("backend/internal/recording/retry.go", "utf8");
  const queryRepository = readFileSync("backend/internal/recording/query_repository.go", "utf8");
  const retryRepository = readFileSync("backend/internal/recording/retry_repository.go", "utf8");

  assert.match(transport, /recordingReader\.Get|recordingV1ResponseFromRecord/);
  assert.match(transport, /recordingRetryService\.Retry/);
  assert.doesNotMatch(transport, /SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue|s\.db/);
  assert.match(queryService, /type RecordRepository interface|type Reader struct/);
  assert.match(retryService, /type RetryUnitOfWork interface|type RetryService struct/);
  assert.doesNotMatch(queryService + retryService, /net\/http|internal\/httpapi|SELECT |INSERT INTO|UPDATE |DELETE FROM|pgx|workqueue/);
  assert.match(queryRepository, /FROM recordings/);
  assert.match(retryRepository, /UPDATE recordings|workqueue\.Enqueue/);
  assert.doesNotMatch(queryRepository + retryRepository, /net\/http|internal\/httpapi|writeJSON/);
});

test("retired recording upload sessions stay removed", () => {
  const deletionRepository = readFileSync("backend/internal/recording/delete_repository.go", "utf8");

  assert.equal(existsSync("backend/internal/storage/recording_sessions.go"), false);
  assert.equal(existsSync("backend/internal/httpapi/recording_sessions_handlers.go"), false);
  assert.doesNotMatch(deletionRepository, /recording_upload_sessions/);
});

test("HTTP transport contains no production SQL or retired Feed surface", () => {
  const transport = readdirSync("backend/internal/httpapi")
    .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
    .map((name) => readFileSync(`backend/internal/httpapi/${name}`, "utf8"))
    .join("\n");
  assert.doesNotMatch(transport, /\bSELECT\b|\bINSERT INTO\b|\bUPDATE\b|\bDELETE FROM\b|s\.db\.(?:Query|QueryRow|Exec|Begin)\(/);
  assert.doesNotMatch(transport, /internal\/db|github\.com\/jackc\/pgx/);
  assert.equal(existsSync("backend/internal/httpapi/feed_handlers.go"), false);
  assert.equal(existsSync("backend/internal/feed/service.go"), false);
  assert.equal(existsSync("backend/internal/httpapi/uploads.go"), false);
});

test("web and backend expose only the versioned application contract", () => {
  const webSource = readdirSync("web/src", { recursive: true })
    .filter((name) => /\.(?:ts|tsx)$/.test(name))
    .map((name) => readFileSync(`web/src/${name}`, "utf8"))
    .join("\n");
  const server = readFileSync("backend/internal/httpapi/server.go", "utf8");

  assert.doesNotMatch(webSource, /["'`]\/api\/(?!v1(?:\/|["'`]))/);
  assert.doesNotMatch(server, /mux\.HandleFunc\("\/api\/(?!v1(?:\/|"))/);
  assert.equal(existsSync("backend/internal/httpapi/uploads.go"), false);
  const feedFiles = existsSync("backend/internal/feed") ? readdirSync("backend/internal/feed") : [];
  assert.deepEqual(feedFiles, []);
});

test("API dependency construction lives in the application composition root", () => {
  const server = readFileSync("backend/internal/httpapi/server.go", "utf8");
  const composition = readFileSync("backend/internal/app/api.go", "utf8");
  const apiMain = readFileSync("backend/cmd/api/main.go", "utf8");

  assert.match(server, /func NewServer\(dependencies Dependencies\)/);
  assert.doesNotMatch(server, /os\.Getenv|ConfigFromEnv|NewSQL|NewLocal|NewLimiter|NewIdentityService|NewRuntime/);
  assert.doesNotMatch(server, /internal\/(?:db|background|worker|transcription|tts|workqueue)/);
  assert.match(composition, /recording\.NewCreator|auth\.NewIdentityService|operations\.NewMonitor/);
  assert.match(composition, /media\.NewService|recording\.NewSQLQueryRepository/);
  assert.doesNotMatch(composition, /feed\.|LegacyUploads|NewLegacyUploads/);
  assert.match(apiMain, /app\.NewAPI\(app\.APIConfig/);
});

test("media ownership and guest policy stay in the media application service", () => {
  const transport = readFileSync("backend/internal/httpapi/media_v1.go", "utf8");
  const service = readFileSync("backend/internal/media/service.go", "utf8");
  const uploadPolicy = readFileSync("backend/internal/media/upload_policy.go", "utf8");
  const model = readFileSync("backend/internal/media/media.go", "utf8");

  assert.match(transport, /media\.DownloadInput/);
  assert.doesNotMatch(transport, /internal\/storage|storage\./);
  assert.doesNotMatch(transport, /requiredMediaUserV1|identity\.Kind\s*!=\s*"user"|PurposeGuestPreviewAudio/);
  assert.match(service, /input\.OwnerKind[\s\S]*ErrAccountRequired/);
  assert.match(uploadPolicy, /case "guest":[\s\S]*ErrGuestRestricted[\s\S]*PurposeGuestPreviewAudio/);
  assert.match(model, /func \(asset Asset\) ClientPurpose\(\)/);
});

test("feature packages own shared vocabulary instead of a catch-all domain package", () => {
  assert.equal(existsSync("backend/internal/domain/domain.go"), false);
  for (const path of [
    "backend/internal/learner/learner.go",
    "backend/internal/media/format.go",
    "backend/internal/media/upload_policy.go",
    "backend/internal/practice/normalization_shared.go",
    "backend/internal/quota/quota.go",
    "backend/internal/recording/normalization.go",
  ]) {
    assert.equal(existsSync(path), true, `missing feature-owned module ${path}`);
  }

  const productionGo = readdirSync("backend/internal", { recursive: true })
    .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
    .map((name) => readFileSync(`backend/internal/${name}`, "utf8"))
    .join("\n");
  assert.doesNotMatch(productionGo, /internal\/domain/);
});

test("guest preview separates transport, processing policy, and SQL storage", () => {
  const model = readFileSync("backend/internal/guestpreview/model.go", "utf8");
  const processor = readFileSync("backend/internal/guestpreview/processor.go", "utf8");
  const store = ["store.go", "processing_store.go"]
    .map((name) => readFileSync(`backend/internal/guestpreview/${name}`, "utf8"))
    .join("\n");
  const transport = readFileSync("backend/internal/httpapi/guest_preview.go", "utf8");

  assert.doesNotMatch(model + processor, /net\/http|internal\/httpapi|pgx|QueryRow|\.Exec\(/);
  assert.match(processor, /ProcessingStore|PreviewAnalyzer/);
  assert.match(store, /guest_previews|processing_jobs|media_assets/);
  assert.doesNotMatch(transport, /SELECT |UPDATE |INSERT INTO|PostChat|ExtractJSONCandidates/);
  assert.match(transport, /guestpreview\.NormalizeCreate|guestPreviewProcessor\.Process/);
});

test("shadowing separates transport, media processing, and persistence", () => {
  const core = ["model.go", "processor.go"]
    .map((name) => readFileSync(`backend/internal/shadowing/${name}`, "utf8"))
    .join("\n");
  const store = readFileSync("backend/internal/shadowing/store.go", "utf8");
  const transport = readFileSync("backend/internal/httpapi/recordings_v1.go", "utf8");

  assert.doesNotMatch(core, /net\/http|internal\/httpapi|pgx|QueryRow|\.Exec\(/);
  assert.match(core, /ProcessingStore|MediaStore/);
  assert.doesNotMatch(core, /LocalSaver|LegacyPublicURL|\/uploads\//);
  assert.match(store, /shadowing_attempt_id|media_assets|processing_jobs/);
  assert.doesNotMatch(transport, /SELECT |UPDATE |INSERT INTO|Synthesize\(|media_assets/);
  assert.doesNotMatch(transport, /pgx|internal\/db/);
  assert.match(transport, /shadowingProcessor\.Process|shadowingStore\.Schedule/);
});

test("unified identity exposes an application service to HTTP transport", () => {
  const service = readFileSync("backend/internal/auth/service.go", "utf8");
  const transport = readFileSync("backend/internal/httpapi/identity_handlers.go", "utf8");

  assert.match(service, /type IdentityService struct/);
  assert.match(service, /CreateAnonymous|Register|Login|Refresh|Authenticate/);
  assert.match(transport, /s\.identityService/);
  assert.doesNotMatch(transport, /s\.db/);
  assert.doesNotMatch(transport, /auth\.(CreateAnonymousIdentity|RegisterIdentityUser|LoginIdentityUser|RotateRefreshToken|AuthenticateAccessToken)/);
});

test("profile and subscription rules stay behind application services", () => {
  const transport = readFileSync("backend/internal/httpapi/user_handlers.go", "utf8");
  const profileService = readFileSync("backend/internal/profile/service.go", "utf8");
  const profileRepository = readFileSync("backend/internal/profile/repository.go", "utf8");
  const subscriptionService = readFileSync("backend/internal/subscription/service.go", "utf8");
  const subscriptionRepository = readFileSync("backend/internal/subscription/repository.go", "utf8");

  assert.match(transport, /profileService\.(EnglishLevel|SaveEnglishLevel|ReplaceInterests)/);
  assert.match(transport, /subscriptionService\.(Get|Activate|Cancel)/);
  assert.doesNotMatch(transport, /DELETE FROM user_interests|INSERT INTO user_interests|subscription_cancelled =|is_subscriber = TRUE/);
  assert.doesNotMatch(profileService + subscriptionService, /SELECT |INSERT INTO|UPDATE |DELETE FROM|net\/http|pgx/);
  assert.match(profileRepository, /user_interests|english_level/);
  assert.match(subscriptionRepository, /subscription_cancelled|subscription_expires_at/);
});
