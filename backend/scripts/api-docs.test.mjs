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
  readFileSync("internal/httpapi/recording_sessions_handlers.go", "utf8"),
  readFileSync("internal/httpapi/feed_handlers.go", "utf8"),
].join("\n");

const httpMethods = new Set(["get", "post", "put", "delete", "patch"]);
const documentedAPIRoutes = [
  "/healthz", "/api/v1", "/api/v1/auth/anonymous", "/api/v1/auth/register", "/api/v1/auth/login",
  "/api/v1/auth/refresh", "/api/v1/auth/session", "/api/v1/auth/logout", "/api/v1/auth/logout-all",
  "/api/v1/auth/sessions", "/api/v1/auth/sessions/{sessionId}",
  "/api/v1/recordings", "/api/v1/recordings/{recordingId}",
  "/api/auth/register", "/api/auth/login", "/api/auth/session", "/api/auth/logout",
  "/api/daily-questions", "/api/topic-guidance", "/api/study-words", "/api/user/data",
  "/api/user/interests", "/api/user/ollama-model", "/api/user/subscription", "/api/user/english-level",
  "/api/user/recordings", "/api/recordings/{recordingId}", "/api/recordings/{recordingId}/retry",
  "/api/recordings/{recordingId}/shadowing", "/api/recording-sessions",
  "/api/recording-sessions/{sessionId}/chunks", "/api/recording-sessions/{sessionId}/audio",
  "/api/recording-sessions/{sessionId}/finish", "/api/feed/posts", "/api/feed/posts/{postId}",
  "/api/feed/posts/{postId}/replies", "/api/feed/posts/{postId}/reactions",
  "/api/feed/replies/{replyId}/reactions", "/uploads/shadowing/{userId}/{fileName}", "/uploads/{path}",
];

const protectedOperations = [
  ["/api/v1/recordings", "get"], ["/api/v1/recordings/{recordingId}", "get"],
  ["/api/auth/session", "get"], ["/api/auth/logout", "post"], ["/api/user/data", "get"],
  ["/api/user/interests", "put"], ["/api/user/ollama-model", "get"], ["/api/user/subscription", "get"],
  ["/api/user/subscription", "post"], ["/api/user/subscription", "delete"],
  ["/api/user/english-level", "get"], ["/api/user/english-level", "put"],
  ["/api/user/recordings", "post"], ["/api/recordings/{recordingId}", "get"],
  ["/api/recordings/{recordingId}", "delete"], ["/api/recordings/{recordingId}/retry", "post"],
  ["/api/recordings/{recordingId}/shadowing", "post"], ["/api/recording-sessions", "post"],
  ["/api/recording-sessions/{sessionId}/chunks", "post"], ["/api/recording-sessions/{sessionId}/audio", "post"],
  ["/api/recording-sessions/{sessionId}/finish", "post"], ["/api/feed/posts", "get"],
  ["/api/feed/posts", "post"], ["/api/feed/posts/{postId}", "get"],
  ["/api/feed/posts/{postId}/replies", "post"], ["/api/feed/posts/{postId}/reactions", "post"],
  ["/api/feed/replies/{replyId}/reactions", "post"], ["/uploads/shadowing/{userId}/{fileName}", "get"],
];

const mutationBodies = [
  ["/api/v1/auth/register", "post", "application/json"], ["/api/v1/auth/login", "post", "application/json"],
  ["/api/v1/auth/refresh", "post", "application/json"],
  ["/api/auth/register", "post", "application/json"], ["/api/auth/login", "post", "application/json"],
  ["/api/user/interests", "put", "application/json"], ["/api/user/english-level", "put", "application/json"],
  ["/api/user/recordings", "post", "application/json"], ["/api/recording-sessions", "post", "application/json"],
  ["/api/recording-sessions/{sessionId}/chunks", "post", "multipart/form-data"],
  ["/api/recording-sessions/{sessionId}/audio", "post", "multipart/form-data"],
  ["/api/recording-sessions/{sessionId}/finish", "post", "application/json"],
  ["/api/feed/posts", "post", "application/json"], ["/api/feed/posts/{postId}/replies", "post", "application/json"],
  ["/api/feed/posts/{postId}/reactions", "post", "application/json"],
  ["/api/feed/replies/{replyId}/reactions", "post", "application/json"],
];

const expectedQueryParameters = new Map([
  ["/api/v1/recordings", ["limit", "cursor"]],
  ["/api/daily-questions", ["date", "refresh", "interest", "level", "avoid"]],
  ["/api/topic-guidance", ["topic", "refresh", "interest", "level", "avoidQuestion", "avoidWord"]],
  ["/api/study-words", ["refresh", "interest", "level", "avoidWord"]],
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

function usesCookieAuth(operation) {
  return (operation.security ?? openapi.security ?? []).some((requirement) => Object.hasOwn(requirement, "cookieAuth"));
}

function usesBearerAuth(operation) {
  return (operation.security ?? openapi.security ?? []).some((requirement) => Object.hasOwn(requirement, "bearerAuth"));
}

test("OpenAPI identifies the API origin and Swagger fetches its served artifact", () => {
  assert.equal(openapi.openapi, "3.1.0");
  assert.equal(openapi.info.title, "DailySpeak API");
  assert.deepEqual(openapi.servers[0], { url: "/", description: "Current API origin" });
  assert.ok(openapi.components.securitySchemes.cookieAuth);
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

test("OpenAPI inventories every API and upload route, including retained Feed endpoints", () => {
  for (const path of documentedAPIRoutes) assert.ok(openapi.paths[path], `missing OpenAPI path ${path}`);
  const sourceChecks = [
    ["/healthz", /mux\.HandleFunc\("\/healthz"/],
    ["/api/v1", /mux\.HandleFunc\("\/api\/v1"/],
    ["/api/v1/auth/anonymous", /path == "\/api\/v1\/auth\/anonymous"/],
    ["/api/v1/auth/sessions/{sessionId}", /strings\.HasPrefix\(path, "\/api\/v1\/auth\/sessions\/"\)/],
    ["/api/v1/recordings", /path == "\/api\/v1\/recordings"/],
    ["/api/v1/recordings/{recordingId}", /strings\.HasPrefix\(path, "\/api\/v1\/recordings\/"\)/],
    ["/api/recording-sessions/{sessionId}/chunks", /action == "chunks"/],
    ["/api/recording-sessions/{sessionId}/audio", /action == "audio"/],
    ["/api/recording-sessions/{sessionId}/finish", /action == "finish"/],
    ["/api/feed/posts", /path == "\/api\/feed\/posts"/],
    ["/api/feed/posts/{postId}", /strings\.HasPrefix\(path, "\/api\/feed\/posts\/"\)/],
    ["/api/feed/posts/{postId}/replies", /parts\[1\] == "replies"/],
    ["/api/feed/posts/{postId}/reactions", /parts\[1\] == "reactions"/],
    ["/api/feed/replies/{replyId}/reactions", /strings\.HasPrefix\(path, "\/api\/feed\/replies\/"\)/],
    ["/uploads/shadowing/{userId}/{fileName}", /mux\.HandleFunc\("\/uploads\/shadowing\/"/],
    ["/uploads/{path}", /mux\.Handle\(uploadsURLPrefix, uploadsHandler\(\)\)/],
  ];
  for (const [path, pattern] of sourceChecks) assert.match(routeSource, pattern, `server route not found for ${path}`);
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

test("protected operations declare cookie authentication", () => {
  for (const [path, method] of protectedOperations) {
    assert.ok(usesCookieAuth(openapi.paths[path][method]), `${method.toUpperCase()} ${path} needs cookieAuth`);
  }
});

test("mobile identity and v1 resources declare bearer authentication", () => {
  const bearerOperations = [
    ["/api/v1/auth/session", "get"], ["/api/v1/auth/logout", "post"],
    ["/api/v1/auth/logout-all", "post"], ["/api/v1/auth/sessions", "get"],
    ["/api/v1/auth/sessions/{sessionId}", "delete"], ["/api/v1/recordings", "get"],
    ["/api/v1/recordings/{recordingId}", "get"],
  ];
  for (const [path, method] of bearerOperations) {
    assert.ok(usesBearerAuth(openapi.paths[path][method]), `${method.toUpperCase()} ${path} needs bearerAuth`);
  }
  assert.deepEqual(openapi.paths["/api/v1/auth/anonymous"].post.security, []);
  assert.deepEqual(openapi.paths["/api/v1/auth/refresh"].post.security, []);
});

test("path and query parameters are declared for every operation that uses them", () => {
  for (const { path, method, operation } of operations()) {
    const parameters = operationParameters(path, operation);
    for (const name of path.matchAll(/\{([^}]+)\}/g)) {
      const parameter = parameters.find((candidate) => candidate.in === "path" && candidate.name === name[1]);
      assert.ok(parameter, `${method.toUpperCase()} ${path} needs path parameter ${name[1]}`);
      assert.equal(parameter.required, true, `${method.toUpperCase()} ${path} path parameter ${name[1]} must be required`);
    }
    for (const name of expectedQueryParameters.get(path) ?? []) {
      assert.ok(parameters.some((parameter) => parameter.in === "query" && parameter.name === name), `${method.toUpperCase()} ${path} needs query parameter ${name}`);
    }
  }
});

test("JSON and multipart mutations declare request bodies", () => {
  for (const [path, method, contentType] of mutationBodies) {
    assert.ok(openapi.paths[path][method].requestBody?.content?.[contentType], `${method.toUpperCase()} ${path} needs ${contentType} request body`);
  }
});

test("shared externally visible schemas have representative examples", () => {
  const expectedObjectSchemas = new Map([
    ["User", ["email", "isSubscriber", "englishLevel"]],
    ["Recording", ["id", "topic", "duration", "timestamp", "status", "transcript", "correctedTranscript", "suggestions", "practiceType", "shadowingStatus", "shadowingAudioUrl", "shadowingError", "shadowingUpdatedAt"]],
    ["Suggestion", ["wrong", "right", "explanation"]],
    ["SubscriptionState", ["isSubscriber", "subscriptionExpiresAt", "subscriptionCancelled"]],
    ["FeedPost", ["id", "sourceRecordingId", "topic", "duration", "transcript", "practiceType", "sourceTimestamp", "createdAt", "authorMaskedEmail", "replyCount", "reactions"]],
    ["FeedReply", ["id", "postId", "duration", "timestamp", "createdAt", "authorMaskedEmail", "reactions"]],
    ["ErrorResponse", ["error"]],
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
