import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const buildAPIDocsScript = fileURLToPath(new URL("./build-api-docs.mjs", import.meta.url));

function runOpenAPICheck(t, source) {
  const workspace = mkdtempSync(join(tmpdir(), "daily-speaking-openapi-"));
  const documentPath = join(workspace, "docs", "openapi.json");
  mkdirSync(dirname(documentPath), { recursive: true });
  writeFileSync(documentPath, source);
  t.after(() => rmSync(workspace, { recursive: true, force: true }));

  return spawnSync(process.execPath, [buildAPIDocsScript, "--check"], {
    cwd: workspace,
    encoding: "utf8",
  });
}

const openapi = JSON.parse(readFileSync("docs/openapi.json", "utf8"));
const swaggerHTML = readFileSync("docs/swagger.html", "utf8");
const compatibilityPolicy = readFileSync("../docs/api-compatibility.md", "utf8");
const serverSource = readFileSync("internal/httpapi/server.go", "utf8");
const routeSource = [
  serverSource,
  readFileSync("internal/httpapi/v1.go", "utf8"),
  readFileSync("internal/httpapi/media_v1.go", "utf8"),
  readFileSync("internal/httpapi/guest_preview.go", "utf8"),
  readFileSync("internal/httpapi/recordings_create_v1.go", "utf8"),
  readFileSync("internal/httpapi/interviews_v1.go", "utf8"),
].join("\n");

const httpMethods = new Set(["get", "post", "put", "delete", "patch"]);
const documentedAPIRoutes = [
  "/healthz", "/readyz", "/api/v1", "/api/v1/auth/anonymous", "/api/v1/auth/register", "/api/v1/auth/login",
  "/api/v1/auth/refresh", "/api/v1/auth/session", "/api/v1/auth/logout", "/api/v1/auth/logout-all",
  "/api/v1/auth/sessions", "/api/v1/auth/sessions/{sessionId}",
  "/api/v1/guest/previews", "/api/v1/guest/previews/{previewId}",
  "/api/v1/interviews", "/api/v1/interviews/{interviewId}",
  "/api/v1/interviews/{interviewId}/start", "/api/v1/interviews/{interviewId}/cancel",
  "/api/v1/interviews/{interviewId}/advance",
  "/api/v1/interviews/{interviewId}/transcription-token",
  "/api/v1/interviews/{interviewId}/question-speech-token",
  "/api/v1/interviews/{interviewId}/turns/{sequence}/audio",
  "/api/v1/interviews/{interviewId}/turns/{sequence}/skip",
  "/api/v1/interviews/{interviewId}/finalize",
  "/api/v1/recordings", "/api/v1/recordings/{recordingId}",
  "/api/v1/recordings/{recordingId}/retry", "/api/v1/recordings/{recordingId}/shadowing",
  "/api/v1/media/uploads", "/api/v1/media/uploads/{uploadId}",
  "/api/v1/media/uploads/{uploadId}/parts", "/api/v1/media/uploads/{uploadId}/complete",
  "/api/v1/media/{assetId}/download", "/api/v1/media/uploads/{uploadId}/parts/{partNumber}",
  "/api/v1/media/local/assets/{assetId}/content",
  "/api/v1/practice/daily-questions", "/api/v1/practice/topic-guidance", "/api/v1/practice/study-words",
  "/api/v1/profile", "/api/v1/profile/interests", "/api/v1/profile/english-level",
  "/api/v1/subscription",
];

const protectedOperations = [
  ["/api/v1/interviews", "post"], ["/api/v1/interviews/{interviewId}", "get"],
  ["/api/v1/interviews/{interviewId}/start", "post"],
  ["/api/v1/interviews/{interviewId}/cancel", "post"],
  ["/api/v1/interviews/{interviewId}/advance", "post"],
  ["/api/v1/interviews/{interviewId}/transcription-token", "post"],
  ["/api/v1/interviews/{interviewId}/question-speech-token", "post"],
  ["/api/v1/interviews/{interviewId}/turns/{sequence}/audio", "post"],
  ["/api/v1/interviews/{interviewId}/turns/{sequence}/skip", "post"],
  ["/api/v1/interviews/{interviewId}/finalize", "post"],
  ["/api/v1/recordings", "get"], ["/api/v1/recordings/{recordingId}", "get"],
  ["/api/v1/recordings/{recordingId}", "delete"],
  ["/api/v1/recordings/{recordingId}/retry", "post"],
  ["/api/v1/recordings/{recordingId}/shadowing", "post"],
  ["/api/v1/profile", "get"], ["/api/v1/profile/interests", "put"],
  ["/api/v1/profile/english-level", "get"], ["/api/v1/profile/english-level", "put"],
  ["/api/v1/subscription", "get"], ["/api/v1/subscription", "post"],
  ["/api/v1/subscription", "delete"],
];

const mutationBodies = [
  ["/api/v1/auth/register", "post", "application/json"], ["/api/v1/auth/login", "post", "application/json"],
  ["/api/v1/auth/refresh", "post", "application/json"],
  ["/api/v1/guest/previews", "post", "application/json"],
  ["/api/v1/interviews", "post", "application/json"],
  ["/api/v1/interviews/{interviewId}/advance", "post", "application/json"],
  ["/api/v1/interviews/{interviewId}/turns/{sequence}/audio", "post", "application/json"],
  ["/api/v1/interviews/{interviewId}/turns/{sequence}/skip", "post", "application/json"],
  ["/api/v1/interviews/{interviewId}/finalize", "post", "application/json"],
  ["/api/v1/recordings", "post", "application/json"],
  ["/api/v1/media/uploads", "post", "application/json"],
  ["/api/v1/media/uploads/{uploadId}/parts", "post", "application/json"],
  ["/api/v1/media/uploads/{uploadId}/complete", "post", "application/json"],
  ["/api/v1/media/uploads/{uploadId}/parts/{partNumber}", "put", "application/octet-stream"],
  ["/api/v1/profile/interests", "put", "application/json"],
  ["/api/v1/profile/english-level", "put", "application/json"],
];

const expectedQueryParameters = new Map([
  ["get /api/v1/recordings", ["limit", "cursor"]],
  ["put /api/v1/media/uploads/{uploadId}/parts/{partNumber}", ["sizeBytes", "checksumSha256", "expires", "signature"]],
  ["get /api/v1/media/local/assets/{assetId}/content", ["expires", "signature"]],
  ["get /api/v1/practice/daily-questions", ["date", "refresh", "interest", "level", "avoid"]],
  ["get /api/v1/practice/topic-guidance", ["topic", "refresh", "interest", "level", "avoidQuestion", "avoidWord"]],
  ["get /api/v1/practice/study-words", ["refresh", "interest", "level", "avoidWord"]],
]);

test("OpenAPI check accepts canonical JSON checked out with Windows line endings", (t) => {
  const result = runOpenAPICheck(t, '{\r\n  "openapi": "3.1.0"\r\n}\r\n');

  assert.equal(result.status, 0, result.stderr);
});

test("OpenAPI check still rejects genuinely noncanonical JSON formatting", (t) => {
  const result = runOpenAPICheck(t, '{"openapi":"3.1.0"}\n');

  assert.equal(result.status, 1);
  assert.match(result.stderr, /OpenAPI formatting drifted/);
});

function operations() {
  return Object.entries(openapi.paths).flatMap(([path, pathItem]) => Object.entries(pathItem)
    .filter(([method]) => httpMethods.has(method))
    .map(([method, operation]) => ({ path, method, operation })));
}

function resolveParameter(parameter) {
  if (!parameter.$ref) return parameter;
  return openapi.components.parameters[parameter.$ref.replace("#/components/parameters/", "")];
}

function operationParameters(path, operation) {
  return [...(openapi.paths[path].parameters ?? []), ...(operation.parameters ?? [])].map(resolveParameter);
}

function usesBearerAuth(operation) {
  return (operation.security ?? openapi.security ?? []).some((requirement) => Object.hasOwn(requirement, "bearerAuth"));
}

test("OpenAPI identifies the API origin and Swagger fetches its served artifact", () => {
  assert.equal(openapi.openapi, "3.1.0");
  assert.equal(openapi.info.title, "DailySpeak API");
  assert.deepEqual(openapi.servers[0], { url: "/", description: "Current API origin" });
  assert.ok(openapi.components.securitySchemes.bearerAuth);
  assert.match(swaggerHTML, /url:\s*"\/openapi\.json"/);
  assert.match(swaggerHTML, /swagger-ui-bundle\.js/);
  assert.match(swaggerHTML, /SwaggerUIBundle/);
  assert.doesNotMatch(swaggerHTML, /openapi\.spec\.js/);
});

test("backend registers GET-only OpenAPI and Swagger routes", () => {
  assert.match(serverSource, /mux\.HandleFunc\("\/openapi\.json"/);
  assert.match(serverSource, /mux\.HandleFunc\("\/docs"/);
  assert.match(serverSource, /r\.Method != http\.MethodGet/);
  assert.match(serverSource, /apidocs\.OpenAPIJSON/);
  assert.match(serverSource, /apidocs\.SwaggerHTML/);
});

test("OpenAPI inventories every supported API route", () => {
  for (const path of documentedAPIRoutes) assert.ok(openapi.paths[path], `missing OpenAPI path ${path}`);
  const sourceChecks = [
    ["/healthz", /mux\.HandleFunc\("\/healthz"/],
    ["/readyz", /mux\.HandleFunc\("\/readyz"/],
    ["/api/v1", /mux\.HandleFunc\("\/api\/v1"/],
    ["/api/v1/auth/anonymous", /path == "\/api\/v1\/auth\/anonymous"/],
    ["/api/v1/auth/sessions/{sessionId}", /strings\.HasPrefix\(path, "\/api\/v1\/auth\/sessions\/"\)/],
    ["/api/v1/guest/previews", /path == "\/api\/v1\/guest\/previews"/],
    ["/api/v1/guest/previews/{previewId}", /strings\.HasPrefix\(path, "\/api\/v1\/guest\/previews\/"\)/],
    ["/api/v1/interviews", /path == "\/api\/v1\/interviews"/],
    ["/api/v1/interviews/{interviewId}", /strings\.HasPrefix\(path, "\/api\/v1\/interviews\/"\)/],
    ["/api/v1/interviews/{interviewId}/start", /parts\[1\] == "start"/],
    ["/api/v1/interviews/{interviewId}/cancel", /parts\[1\] == "cancel"/],
    ["/api/v1/interviews/{interviewId}/advance", /parts\[1\] == "advance"/],
    ["/api/v1/interviews/{interviewId}/transcription-token", /parts\[1\] == "transcription-token"/],
    ["/api/v1/interviews/{interviewId}/question-speech-token", /parts\[1\] == "question-speech-token"/],
    ["/api/v1/interviews/{interviewId}/turns/{sequence}/audio", /parts\[3\] == "audio"/],
    ["/api/v1/interviews/{interviewId}/turns/{sequence}/skip", /parts\[3\] == "skip"/],
    ["/api/v1/interviews/{interviewId}/finalize", /parts\[1\] == "finalize"/],
    ["/api/v1/recordings", /path == "\/api\/v1\/recordings"/],
    ["/api/v1/recordings/{recordingId}", /strings\.HasPrefix\(path, "\/api\/v1\/recordings\/"\)/],
    ["/api/v1/media/uploads", /path == "\/api\/v1\/media\/uploads"/],
    ["/api/v1/media/uploads/{uploadId}", /strings\.HasPrefix\(path, "\/api\/v1\/media\/uploads\/"\)/],
    ["/api/v1/media/uploads/{uploadId}/parts", /parts\[1\] == "parts"/],
    ["/api/v1/media/uploads/{uploadId}/complete", /parts\[1\] == "complete"/],
    ["/api/v1/media/{assetId}/download", /strings\.HasSuffix\(path, "\/download"\)/],
    ["/api/v1/media/uploads/{uploadId}/parts/{partNumber}", /mux\.HandleFunc\("\/api\/v1\/media\/uploads\/"/],
    ["/api/v1/media/local/assets/{assetId}/content", /mux\.HandleFunc\("\/api\/v1\/media\/local\/"/],
    ["/api/v1/practice/daily-questions", /path == "\/api\/v1\/practice\/daily-questions"/],
    ["/api/v1/profile", /path == "\/api\/v1\/profile"/],
    ["/api/v1/subscription", /path == "\/api\/v1\/subscription"/],
  ];
  for (const [path, pattern] of sourceChecks) assert.match(routeSource, pattern, `server route not found for ${path}`);
});

test("OpenAPI exposes only the versioned application contract", () => {
  for (const path of Object.keys(openapi.paths)) {
    if (path.startsWith("/api/")) {
      assert.match(path, /^\/api\/v1(?:\/|$)/, `unversioned API path returned: ${path}`);
    }
  }
  for (const retiredSchema of ["FeedPost", "FeedReply", "FeedReaction", "AudioDataUrl", "PhotoDataUrl"]) {
    assert.equal(openapi.components.schemas[retiredSchema], undefined, `retired schema returned: ${retiredSchema}`);
  }
});

test("every documented operation has a summary, success response, and applicable shared error response", () => {
  for (const { path, method, operation } of operations()) {
    assert.ok(operation.summary?.trim(), `${method.toUpperCase()} ${path} needs a summary`);
    assert.ok(Object.keys(operation.responses ?? {}).some((status) => /^2\d\d$/.test(status)), `${method.toUpperCase()} ${path} needs a success response`);
    if (path.startsWith("/api/")) {
      assert.ok(Object.entries(operation.responses).some(([status, response]) => /^[45]\d\d$/.test(status) && response.$ref?.startsWith("#/components/responses/")), `${method.toUpperCase()} ${path} needs a shared error response`);
    }
  }
});

test("all protected operations declare Bearer authentication", () => {
  for (const [path, method] of protectedOperations) {
    assert.ok(usesBearerAuth(openapi.paths[path][method]), `${method.toUpperCase()} ${path} needs bearerAuth`);
  }
});

test("identity and v1 resources declare bearer authentication", () => {
  const bearerOperations = [
    ["/api/v1/auth/session", "get"], ["/api/v1/auth/logout", "post"],
    ["/api/v1/auth/logout-all", "post"], ["/api/v1/auth/sessions", "get"],
    ["/api/v1/auth/sessions/{sessionId}", "delete"], ["/api/v1/recordings", "get"],
    ["/api/v1/guest/previews", "post"], ["/api/v1/guest/previews/{previewId}", "get"],
    ["/api/v1/interviews", "post"], ["/api/v1/interviews/{interviewId}", "get"],
    ["/api/v1/interviews/{interviewId}/start", "post"],
    ["/api/v1/interviews/{interviewId}/cancel", "post"],
    ["/api/v1/interviews/{interviewId}/advance", "post"],
    ["/api/v1/interviews/{interviewId}/transcription-token", "post"],
    ["/api/v1/interviews/{interviewId}/question-speech-token", "post"],
    ["/api/v1/interviews/{interviewId}/turns/{sequence}/audio", "post"],
    ["/api/v1/interviews/{interviewId}/turns/{sequence}/skip", "post"],
    ["/api/v1/interviews/{interviewId}/finalize", "post"],
    ["/api/v1/recordings", "post"], ["/api/v1/recordings/{recordingId}", "get"],
    ["/api/v1/recordings/{recordingId}", "delete"],
    ["/api/v1/recordings/{recordingId}/retry", "post"],
    ["/api/v1/recordings/{recordingId}/shadowing", "post"],
    ["/api/v1/media/uploads", "post"], ["/api/v1/media/uploads/{uploadId}", "get"],
    ["/api/v1/media/uploads/{uploadId}", "delete"], ["/api/v1/media/uploads/{uploadId}/parts", "post"],
    ["/api/v1/media/uploads/{uploadId}/complete", "post"], ["/api/v1/media/{assetId}/download", "get"],
  ];
  for (const [path, method] of bearerOperations) {
    assert.ok(usesBearerAuth(openapi.paths[path][method]), `${method.toUpperCase()} ${path} needs bearerAuth`);
  }
  assert.deepEqual(openapi.paths["/api/v1/auth/anonymous"].post.security, []);
  assert.deepEqual(openapi.paths["/api/v1/auth/refresh"].post.security, []);
  assert.equal(openapi.components.securitySchemes.cookieAuth, undefined);
  assert.deepEqual(openapi.security, [{ bearerAuth: [] }]);
  assert.equal(openapi.components.schemas.RefreshTokenInput.required, undefined);
  assert.ok(!openapi.components.schemas.IdentityTokens.required.includes("refreshToken"));
  assert.ok(openapi.components.responses.V1IdentityCreated.headers["Set-Cookie"]);
});

test("path and query parameters are declared for every operation that uses them", () => {
  for (const { path, method, operation } of operations()) {
    const parameters = operationParameters(path, operation);
    for (const name of path.matchAll(/\{([^}]+)\}/g)) {
      const parameter = parameters.find((candidate) => candidate.in === "path" && candidate.name === name[1]);
      assert.ok(parameter, `${method.toUpperCase()} ${path} needs path parameter ${name[1]}`);
      assert.equal(parameter.required, true, `${method.toUpperCase()} ${path} path parameter ${name[1]} must be required`);
    }
    for (const name of expectedQueryParameters.get(`${method} ${path}`) ?? []) {
      assert.ok(parameters.some((parameter) => parameter.in === "query" && parameter.name === name), `${method.toUpperCase()} ${path} needs query parameter ${name}`);
    }
  }
});

test("JSON and multipart mutations declare request bodies", () => {
  for (const [path, method, contentType] of mutationBodies) {
    assert.ok(openapi.paths[path][method].requestBody?.content?.[contentType], `${method.toUpperCase()} ${path} needs ${contentType} request body`);
  }
});

test("adaptive interview contract keeps final speech separate from question metadata", () => {
  const interview = openapi.components.schemas.InterviewSession;
  const vocabularyItem = openapi.components.schemas.InterviewVocabularyItem;
  const savedTurn = openapi.components.schemas.SavedInterviewTurn;
  assert.ok(interview.properties.candidates);
  assert.ok(interview.properties.turns);
  assert.equal(interview.properties.usefulWords.maxItems, 12);
  assert.equal(interview.properties.usefulVocabulary.maxItems, 12);
  assert.equal(interview.properties.usefulVocabulary.items.$ref, "#/components/schemas/InterviewVocabularyItem");
  assert.deepEqual(vocabularyItem.required, ["word", "translation"]);
  assert.deepEqual(openapi.components.schemas.InterviewTurn.properties.questionSource.enum, ["opening", "prepared", "adaptive"]);
  assert.ok(savedTurn.required.includes("question"));
  assert.ok(savedTurn.required.includes("answerText"));
  assert.ok(openapi.components.schemas.V1Recording.properties.interviewTurns);
  assert.ok(openapi.components.schemas.GuestPreview.properties.interviewTurns);
  assert.ok(openapi.components.schemas.CreateRecordingFromAssetsRequest.properties.interviewSessionId);
  assert.ok(openapi.components.schemas.CreateGuestPreviewRequest.properties.interviewSessionId);
  assert.ok(openapi.components.schemas.MediaPurpose.enum.includes("interview_turn_audio"));
  assert.ok(openapi.components.schemas.CreateMediaUploadRequest.properties.interviewSessionId);
  assert.equal(openapi.components.schemas.AdvanceInterviewRequest.properties.skipCurrent.type, "boolean");
  assert.equal(
    openapi.paths["/api/v1/interviews/{interviewId}/turns/{sequence}/skip"].post.requestBody.content["application/json"].schema.$ref,
    "#/components/schemas/SkipInterviewTurnRequest",
  );
});

test("shared externally visible schemas have representative examples", () => {
  const expectedObjectSchemas = new Map([
    ["User", ["email", "isSubscriber", "englishLevel"]],
    ["V1Recording", ["id", "topic", "duration", "timestamp", "status", "transcript", "correctedTranscript", "suggestions", "practiceType", "shadowingStatus", "shadowingError", "shadowingUpdatedAt"]],
    ["Suggestion", ["wrong", "right", "explanation"]],
    ["SubscriptionState", ["isSubscriber", "subscriptionExpiresAt", "subscriptionCancelled"]],
    ["MediaAsset", ["id", "state", "purpose", "contentType", "sizeBytes", "checksum"]],
    ["MediaUpload", ["id", "state", "partSizeBytes", "partCount", "expiresAt", "uploadedParts"]],
    ["V1ErrorResponse", ["error"]],
    ["GuestPreview", ["id", "state", "topic", "duration", "timestamp", "practiceType", "transcript", "corrections", "expiresAt", "createdAt", "updatedAt"]],
  ]);
  for (const [name, fields] of expectedObjectSchemas) {
    const schema = openapi.components.schemas[name];
    assert.ok(schema, `missing ${name} schema`);
    assert.ok(schema.properties && typeof schema.properties === "object", `${name} needs properties`);
    assert.ok(Array.isArray(schema.required), `${name} needs a required field list`);
    assert.ok(schema.example && typeof schema.example === "object", `${name} needs an object example`);
    for (const field of fields) {
      assert.ok(Object.hasOwn(schema.properties, field), `${name} properties need ${field}`);
      assert.ok(schema.required.includes(field), `${name} must require ${field}`);
      assert.ok(Object.hasOwn(schema.example, field), `${name} example needs ${field}`);
    }
  }
  const processingSchema = openapi.components.schemas.RecordingProcessingStage;
  assert.ok(processingSchema, "missing RecordingProcessingStage schema");
  assert.ok(processingSchema.example, "RecordingProcessingStage needs an example");
});

test("v1 recording contracts expose only protected media references", () => {
  const recording = openapi.components.schemas.V1Recording;
  for (const legacyField of ["audioDataUrl", "photoDataUrl", "shadowingAudioUrl"]) {
    assert.ok(!Object.hasOwn(recording.properties, legacyField), `V1Recording must not expose ${legacyField}`);
  }
  assert.equal(
    openapi.components.schemas.V1RecordingResponse.properties.recording.$ref,
    "#/components/schemas/V1Recording",
  );
  assert.equal(
    openapi.components.schemas.RecordingPage.properties.items.items.$ref,
    "#/components/schemas/V1Recording",
  );
  assert.equal(openapi.components.schemas.Recording, undefined);
  assert.equal(openapi.components.schemas.RecordingResponse, undefined);
});

test("v1 operations expose request IDs and structured stable errors", () => {
  const v1Operations = operations().filter(({ path }) => path.startsWith("/api/v1"));
  assert.ok(v1Operations.length >= 3);
  for (const { path, method, operation } of v1Operations) {
    const parameters = operationParameters(path, operation);
    assert.ok(parameters.some((parameter) => parameter.in === "header" && parameter.name === "X-Request-ID"), `${method.toUpperCase()} ${path} needs X-Request-ID request parameter`);
    for (const [status, responseReference] of Object.entries(operation.responses)) {
      assert.ok(responseReference.$ref?.startsWith("#/components/responses/V1"), `${method.toUpperCase()} ${path} ${status} must use a v1 shared response`);
      const response = openapi.components.responses[responseReference.$ref.replace("#/components/responses/", "")];
      assert.ok(response.headers?.["X-Request-ID"], `${method.toUpperCase()} ${path} ${status} needs X-Request-ID response header`);
      if (/^[45]\d\d$/.test(status)) {
        assert.equal(response.content?.["application/json"]?.schema?.$ref, "#/components/schemas/V1ErrorResponse", `${method.toUpperCase()} ${path} ${status} needs structured v1 error`);
      }
    }
  }
  assert.match(compatibilityPolicy, /at least 90 days/);
  assert.match(compatibilityPolicy, /new major path/);
});

test("mobile media contract keeps mutations idempotent and storage requests opaque", () => {
  for (const [path, method] of [
    ["/api/v1/media/uploads", "post"],
    ["/api/v1/recordings", "post"],
    ["/api/v1/guest/previews", "post"],
  ]) {
    const parameters = operationParameters(path, openapi.paths[path][method]);
    const key = parameters.find((parameter) => parameter.in === "header" && parameter.name === "Idempotency-Key");
    assert.ok(key?.required, `${method.toUpperCase()} ${path} needs required Idempotency-Key`);
  }

  const createUpload = openapi.paths["/api/v1/media/uploads"].post;
  assert.equal(createUpload.responses["403"].$ref, "#/components/responses/V1Forbidden");
  assert.equal(createUpload.responses["415"].$ref, "#/components/responses/V1UnsupportedMediaType");
  assert.equal(createUpload.responses["422"].$ref, "#/components/responses/V1UnprocessableEntity");

  const signedPart = openapi.paths["/api/v1/media/uploads/{uploadId}/parts/{partNumber}"].put;
  const signedContent = openapi.paths["/api/v1/media/local/assets/{assetId}/content"].get;
  assert.deepEqual(signedPart.security, []);
  assert.deepEqual(signedContent.security, []);
  assert.equal(signedPart.requestBody.content["application/octet-stream"].schema.format, "binary");
  assert.match(openapi.components.schemas.SignedMediaRequest.properties.url.description, /opaque/i);
  assert.match(openapi.components.schemas.SignedMediaRequest.properties.url.description, /Do not persist/i);

  const guestPreview = openapi.components.schemas.GuestPreview;
  assert.equal(guestPreview.properties.corrections.maxItems, 2);
  assert.equal(guestPreview.properties.duration.maximum, 180);
  assert.equal(openapi.components.schemas.CreateGuestPreviewRequest.properties.duration.maximum, 180);
  assert.deepEqual(guestPreview.properties.state.$ref, "#/components/schemas/GuestPreviewState");
  assert.equal(openapi.paths["/api/v1/guest/previews"].post.responses["503"].$ref, "#/components/responses/V1CapacityUnavailable");
  assert.deepEqual(openapi.components.schemas.IdentityGrantResponse.properties.guestPreviewPromotion.$ref, "#/components/schemas/GuestPreviewPromotion");
  assert.deepEqual(openapi.components.schemas.GuestPreviewPromotion.properties.status.enum, ["promoted", "not_promoted", "no_preview"]);
  assert.deepEqual(openapi.components.schemas.GuestPreviewPromotion.properties.reason.enum, ["quota_exceeded"]);

  const accountDuration = openapi.components.schemas.CreateRecordingFromAssetsRequest.properties.duration;
  const recordingQuota = openapi.components.schemas.RecordingQuota.properties;
  assert.equal(accountDuration.maximum, 600);
  assert.equal(recordingQuota.maxSessionSeconds.maximum, 600);
  assert.match(recordingQuota.weeklyLimitSeconds.description, /Legacy compatibility value: 600 for non-subscribers/);
  assert.match(recordingQuota.weeklyRemainingSeconds.description, /never decremented/);
  assert.match(recordingQuota.weeklyRemainingSeconds.description, /does not control recording admission/);
  assert.equal(openapi.components.schemas.InterviewSession.properties.maxDurationSeconds.maximum, 600);
  assert.match(openapi.components.schemas.CreateMediaUploadRequest.properties.sizeBytes.description, /8 MiB for a guest/);
});
