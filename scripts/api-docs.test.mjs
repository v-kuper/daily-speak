import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const openapi = JSON.parse(readFileSync("docs/api/openapi.json", "utf8"));
const generatedSpec = readFileSync("docs/api/openapi.spec.js", "utf8");
const swaggerHtml = readFileSync("docs/api/swagger.html", "utf8");
const serverSource = readFileSync("backend/internal/httpapi/server.go", "utf8");
const routeSource = [
  serverSource,
  readFileSync("backend/internal/httpapi/recording_sessions_handlers.go", "utf8"),
  readFileSync("backend/internal/httpapi/feed_handlers.go", "utf8"),
].join("\n");

const expectedGeneratedSpec = `window.DAILY_SPEAKING_OPENAPI = ${JSON.stringify(openapi, null, 2)};\n`;

const documentedPaths = [
  "/healthz",
  "/api/auth/register",
  "/api/auth/login",
  "/api/auth/session",
  "/api/auth/logout",
  "/api/daily-questions",
  "/api/topic-guidance",
  "/api/study-words",
  "/api/user/data",
  "/api/user/interests",
  "/api/user/ollama-model",
  "/api/user/subscription",
  "/api/user/english-level",
  "/api/user/recordings",
  "/api/recordings/{recordingId}",
  "/api/recording-sessions",
  "/api/recording-sessions/{sessionId}/chunks",
  "/api/recording-sessions/{sessionId}/finish",
  "/api/feed/posts",
  "/api/feed/posts/{postId}",
  "/api/feed/posts/{postId}/replies",
  "/api/feed/posts/{postId}/reactions",
  "/api/feed/replies/{replyId}/reactions",
  "/uploads/{path}",
];

test("OpenAPI document is valid JSON and has the expected version", () => {
  assert.equal(openapi.openapi, "3.1.0");
  assert.equal(openapi.info.title, "DailySpeak API");
  assert.ok(openapi.components.securitySchemes.cookieAuth);
});

test("Swagger HTML loads the generated local spec and Swagger UI", () => {
  assert.match(swaggerHtml, /openapi\.spec\.js/);
  assert.match(swaggerHtml, /swagger-ui-bundle\.js/);
  assert.match(swaggerHtml, /SwaggerUIBundle/);
});

test("generated Swagger spec JS matches docs/api/openapi.json", () => {
  assert.equal(generatedSpec, expectedGeneratedSpec);
});

test("OpenAPI documents all public gateway routes", () => {
  for (const path of documentedPaths) {
    assert.ok(openapi.paths[path], `missing OpenAPI path ${path}`);
  }
});

test("OpenAPI stays aligned with server.go route literals", () => {
  const serverRouteChecks = [
    ["/healthz", /mux\.HandleFunc\("\/healthz"/],
    ["/api/auth/register", /path == "\/api\/auth\/register"/],
    ["/api/auth/login", /path == "\/api\/auth\/login"/],
    ["/api/auth/session", /path == "\/api\/auth\/session"/],
    ["/api/auth/logout", /path == "\/api\/auth\/logout"/],
    ["/api/daily-questions", /path == "\/api\/daily-questions"/],
    ["/api/topic-guidance", /path == "\/api\/topic-guidance"/],
    ["/api/study-words", /path == "\/api\/study-words"/],
    ["/api/user/data", /path == "\/api\/user\/data"/],
    ["/api/user/interests", /path == "\/api\/user\/interests"/],
    ["/api/user/ollama-model", /path == "\/api\/user\/ollama-model"/],
    ["/api/user/subscription", /path == "\/api\/user\/subscription"/],
    ["/api/user/english-level", /path == "\/api\/user\/english-level"/],
    ["/api/user/recordings", /path == "\/api\/user\/recordings"/],
    ["/api/recordings/{recordingId}", /strings\.HasPrefix\(path, "\/api\/recordings\/"\)/],
    ["/api/recording-sessions", /path == "\/api\/recording-sessions"/],
    ["/api/recording-sessions/{sessionId}/chunks", /action == "chunks"/],
    ["/api/recording-sessions/{sessionId}/finish", /action == "finish"/],
    ["/api/feed/posts", /path == "\/api\/feed\/posts"/],
    ["/api/feed/posts/{postId}", /strings\.HasPrefix\(path, "\/api\/feed\/posts\/"\)/],
    ["/api/feed/posts/{postId}/replies", /parts\[1\] == "replies"/],
    ["/api/feed/posts/{postId}/reactions", /parts\[1\] == "reactions"/],
    ["/api/feed/replies/{replyId}/reactions", /strings\.HasPrefix\(path, "\/api\/feed\/replies\/"\)/],
    ["/uploads/{path}", /mux\.Handle\(uploadsURLPrefix, uploadsHandler\(\)\)/],
  ];

  for (const [path, routePattern] of serverRouteChecks) {
    assert.match(routeSource, routePattern, `server route not found for ${path}`);
    assert.ok(openapi.paths[path], `OpenAPI path not found for ${path}`);
  }
});
