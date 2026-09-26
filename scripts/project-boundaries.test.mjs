import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
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
  assert.match(documentationIndex, /ARCHITECTURE\.md/);
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
    "backend/public/uploads/feed-replies/user/reply.webm",
    "backend/public/uploads/shadowing/user/pronunciation.mp3",
  ]) {
    const result = spawnSync("git", ["check-ignore", "--quiet", "--no-index", path]);
    assert.equal(result.status, 0, `${path} must be ignored by the repository Git configuration`);
  }
});

test("backend environment example exposes the supported Whisper initial prompt", () => {
  const env = Object.fromEntries(readFileSync("backend/.env.example", "utf8")
    .split(/\r?\n/)
    .filter((line) => line && !line.startsWith("#") && line.includes("="))
    .map((line) => {
      const separator = line.indexOf("=");
      return [line.slice(0, separator), line.slice(separator + 1).replace(/^"|"$/g, "")];
    }));
  const whisper = readFileSync("backend/internal/transcription/whisper.go", "utf8");

  assert.match(whisper, /os\.Getenv\("WHISPER_INITIAL_PROMPT"\)/);
  assert.match(env.WHISPER_INITIAL_PROMPT ?? "", /English.*Russian.*Cyrillic/);
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
  const legacyWorkers = readFileSync("backend/internal/httpapi/durable_workers.go", "utf8");
  const mediaCleanup = readFileSync("backend/internal/media/cleanup.go", "utf8");
	const mediaMaterializer = readFileSync("backend/internal/media/materializer.go", "utf8");

  assert.doesNotMatch(apiMain, /RunWorkers|StartBackgroundWorkers/);
  assert.match(workerMain, /RunWorkers/);
  assert.match(workerMain, /worker\.ConfigFromEnv/);
  assert.match(runtime, /workqueue\.Run/);
  assert.doesNotMatch(legacyWorkers, /WorkerConfigFromEnv|workqueue\.Run/);
  assert.match(mediaCleanup, /AbortExpiredUploads|EnqueueExpiredAssets|FinalizeFailure/);
  assert.doesNotMatch(legacyWorkers, /storage_driver|pending_file_deletions/);
	assert.match(mediaMaterializer, /verified_checksum_sha256|io\.LimitReader/);
  assert.equal(existsSync("backend/internal/httpapi/media_workers.go"), false);
	assert.equal(existsSync("backend/internal/httpapi/guest_preview_probe.go"), false);
	assert.match(readFileSync("backend/internal/media/probe.go", "utf8"), /func ProbeAudioDuration/);
	assert.match(readFileSync("backend/internal/storage/legacy.go", "utf8"), /type LegacyUploads/);
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
	const processingTransport = readFileSync("backend/internal/httpapi/recording_processing.go", "utf8");
	const processingRepository = readFileSync("backend/internal/recording/processing_repository.go", "utf8");
	assert.match(processingTransport, /recording\.ProcessingJob/);
	assert.doesNotMatch(processingTransport, /SELECT |UPDATE |INSERT INTO|processing_stage/);
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
  const core = ["model.go", "local.go", "processor.go"]
    .map((name) => readFileSync(`backend/internal/shadowing/${name}`, "utf8"))
    .join("\n");
  const store = readFileSync("backend/internal/shadowing/store.go", "utf8");
  const transport = readFileSync("backend/internal/httpapi/shadowing.go", "utf8");

  assert.doesNotMatch(core, /net\/http|internal\/httpapi|pgx|QueryRow|\.Exec\(/);
  assert.match(core, /ProcessingStore|MediaStore|LocalSaver/);
  assert.match(store, /shadowing_attempt_id|media_assets|processing_jobs/);
  assert.doesNotMatch(transport, /SELECT |UPDATE |INSERT INTO|Synthesize\(|media_assets/);
  assert.match(transport, /shadowingProcessor\.Process|shadowingStore\.Schedule/);
});
