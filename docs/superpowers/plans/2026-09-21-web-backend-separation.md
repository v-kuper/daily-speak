# Web and Backend Separation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the repository into standalone Next.js web and Go backend projects, add URL-based routing and a direct cross-origin API client, remove Feed from the web UI, preserve the existing cookie session and Windows deployment, and serve the backend contract through Swagger.

**Architecture:** The monorepo keeps root-level orchestration, but `web/` and `backend/` own independent dependency graphs, builds, tests, and Dockerfiles. The browser calls a runtime-configured public API origin directly; the Go server exposes no Next.js proxy. Docker Compose and the existing Caddy infrastructure deploy both applications on one test host while preserving the ability to move them to separate resources later.

**Tech Stack:** Next.js 15.2, React 19, TypeScript 5.7, Redux Toolkit 2.5, Node.js 22 test runner, Go 1.25 module with Go 1.26.2 Docker builder, PostgreSQL 16, Docker Compose, Caddy 2, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-21-web-backend-separation-design.md`

## Global Constraints

- Keep the existing PostgreSQL schema, session-token format, session-cookie name, uploads directory contents, and Feed backend data intact.
- Do not implement access tokens, refresh tokens, OAuth, PKCE, native mobile authentication, or `/api/v1` in this plan.
- Browser requests must call `PUBLIC_API_BASE_URL` directly; neither application may reverse proxy the other.
- Preserve web host ports `3218`/`3443`; add backend host ports `3219`/`3444`.
- `web/` must build without Go/backend source; `backend/` must build without Node/Next.js source.
- Feed handlers and OpenAPI operations remain on the backend while every Feed entry point and publication/comment surface is removed from web.
- Use test-first red-green-refactor cycles for every behavior change and run the complete project suite before completion.
- Keep Node.js at major version 22, PostgreSQL at major version 16, and the existing self-hosted Windows runner labels and `main`/`master` deployment triggers.

## Review Focus

- A session created on API HTTPS `3444` must be sent by a page on web HTTPS `3443`, while an unlisted origin must not receive credentialed CORS access.
- Chunked audio, fallback data-URL audio, photo `FormData`, abort signals, and `/uploads/*` media URLs must all use the API origin without body corruption or double-prefixing.
- Direct visits and refreshes on every protected route must wait for session restoration, avoid redirect loops, and preserve only validated internal `returnTo` values.
- An optimistic `local-*` recording route must be replaced by the permanent recording ID after save and must not leave an unreloadable URL after terminal failure.
- The Windows deploy must preserve its external uploads directory and PostgreSQL volume while producing useful, separate web/backend/Caddy logs on failure.

---

### Task 1: Establish the two application project boundaries

**Files:**
- Create: `scripts/project-boundaries.test.mjs`
- Create: `web/package.json`
- Create: `web/package-lock.json`
- Create: `web/.dockerignore`
- Create: `backend/.dockerignore`
- Modify: `package.json`
- Modify: `package-lock.json`
- Modify: `.gitignore`
- Modify: `.dockerignore`
- Move: `app/` to `web/app/`
- Move: `src/` to `web/src/`
- Move: `next.config.ts`, `tsconfig.json`, `eslint.config.mjs`, `next-env.d.ts` to `web/`
- Move: client test files from `scripts/` to `web/scripts/`
- Move: `Daily Speaking Practice.html` and `design-qa.md` to `web/`
- Move: `scripts/setup-whisper-openai-local.sh`, `scripts/check-whisper-local.sh`, `tools/` to `backend/`

**Interfaces:**
- Consumes: the current root Next.js package and the existing `backend/go.mod` module.
- Produces: `npm --prefix web ...` commands for the client, `go -C backend ...` ownership for the API, and root compatibility aliases that orchestrate but do not own runtime dependencies.

- [ ] **Step 1: Write the failing repository-boundary test**

Create `scripts/project-boundaries.test.mjs`:

```js
import assert from "node:assert/strict";
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

test("frontend source is not left at repository root", () => {
  assert.equal(existsSync("app"), false);
  assert.equal(existsSync("src"), false);
  assert.equal(existsSync("next.config.ts"), false);
});
```

- [ ] **Step 2: Run the boundary test and verify RED**

Run: `node --test scripts/project-boundaries.test.mjs`

Expected: FAIL because `web/package.json`, `web/app/layout.tsx`, and both standalone Dockerfiles do not exist yet.

- [ ] **Step 3: Move project-owned files and create root compatibility commands**

Move the client application, its TypeScript configuration, and these client tests into `web/scripts/`:

```text
browser-media.test.mjs
interview-guidance.test.mjs
recording-deletion.test.mjs
recording-processing.test.mjs
recording-upload-flow.test.mjs
shadowing.test.mjs
suggestion-presentation.test.mjs
suggestions.test.mjs
transcript-highlight.test.mjs
```

Update moved tests so paths such as `src/lib/browserMedia.ts` remain relative to the `web/` working directory. Move Whisper scripts and `tools/` under `backend/`, updating their root calculation so local setup writes `backend/.venv`, `backend/tools/whisper`, and `backend/tools/ffmpeg`.

Replace the root `package.json` with orchestration-only commands:

```json
{
  "name": "daily-speaking-workspace",
  "version": "1.0.0",
  "private": true,
  "scripts": {
    "dev": "npm run dev --prefix web",
    "dev:api": "cd backend && go run ./cmd/api",
    "web:quality": "npm run quality --prefix web",
    "backend:test": "cd backend && go test ./...",
    "test:api-docs": "node --test scripts/api-docs.test.mjs",
    "test:ci": "node --test scripts/project-boundaries.test.mjs scripts/ci-workflows.test.mjs",
    "test:smoke": "node scripts/smoke-api.mjs",
    "quality": "npm run web:quality && npm run backend:test && npm run test:api-docs && npm run test:ci",
    "docker:app": "docker compose up --build -d web backend postgres",
    "docker:lan": "node scripts/docker-lan.mjs",
    "docker:build": "docker compose build web backend",
    "docker:logs": "docker compose logs -f web backend",
    "docker:stop": "docker compose down",
    "setup:whisper": "bash backend/scripts/setup-whisper-openai-local.sh",
    "check:whisper": "bash backend/scripts/check-whisper-local.sh"
  }
}
```

In `web/package.json`, keep the application dependencies and define a web-only quality command:

```json
{
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "typecheck": "tsc --noEmit",
    "lint": "eslint .",
    "test": "node --test scripts/*.test.mjs",
    "quality": "npm run typecheck && npm run lint && npm test"
  }
}
```

Regenerate the root lockfile with no dependencies by running `npm install --package-lock-only --ignore-scripts`, and retain the moved client lockfile as `web/package-lock.json`. Keep the API-doc command pointed at the root script until Task 4 moves the contract into the backend. Scope `.gitignore` and `.dockerignore` entries for both `web/.next`, `web/node_modules`, `backend/.venv`, and `backend/tools` caches.

Create minimal valid standalone Dockerfiles sufficient for the boundary test; Tasks 2 and 9 replace them with final multi-stage builds. Add project-local `.dockerignore` files because Docker applies ignore rules from each build context: `web/.dockerignore` excludes `.next`, `node_modules`, coverage, and logs; `backend/.dockerignore` excludes `.cache`, `.venv`, uploads, compiled binaries, and Whisper model/cache content while retaining tracked `.gitkeep` files.

```dockerfile
# web/Dockerfile
FROM node:22-alpine
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
CMD ["npm", "start"]
```

```dockerfile
# backend/Dockerfile
FROM golang:1.26.2-alpine
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /daily-speaking-api ./cmd/api
CMD ["/daily-speaking-api"]
```

- [ ] **Step 4: Verify GREEN for project boundaries and existing isolated suites**

Run:

```bash
node --test scripts/project-boundaries.test.mjs
npm ci --prefix web
npm run quality --prefix web
cd backend && go test ./...
```

Expected: all commands PASS. Client tests resolve files from `web/`; Go tests remain unchanged.

- [ ] **Step 5: Commit the project boundary move**

```bash
git add package.json package-lock.json .gitignore .dockerignore web backend scripts/project-boundaries.test.mjs
git commit -m "refactor: split web and backend projects"
```

---

### Task 2: Remove the Next.js gateway responsibility from Go

**Files:**
- Modify: `backend/internal/httpapi/contract_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/cmd/api/main.go`
- Modify: `scripts/smoke-api.mjs`
- Modify: `scripts/project-boundaries.test.mjs`

**Interfaces:**
- Consumes: `httpapi.NewServer(httpapi.Config{DB: database})` and existing API/upload handlers.
- Produces: a backend whose public surface is API, uploads, health, and docs only; unknown web paths return JSON `404`.

- [ ] **Step 1: Write failing backend boundary tests**

Add to `backend/internal/httpapi/contract_test.go`:

```go
func TestBackendDoesNotServeOrProxyWebRoutes(t *testing.T) {
	handler := NewServer(Config{}).Handler()

	for _, path := range []string{"/", "/speak", "/history/demo"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", path, recorder.Code)
		}
		if strings.TrimSpace(recorder.Body.String()) != `{"error":"Not found"}` {
			t.Fatalf("%s: unexpected body %q", path, recorder.Body.String())
		}
	}
}
```

Extend `scripts/project-boundaries.test.mjs`:

```js
test("backend source has no Next.js upstream", () => {
  const main = readFileSync("backend/cmd/api/main.go", "utf8");
  const server = readFileSync("backend/internal/httpapi/server.go", "utf8");
  assert.doesNotMatch(main, /NEXT_UPSTREAM_URL|NextURL|proxying Next/);
  assert.doesNotMatch(server, /httputil|NewSingleHostReverseProxy|nextProxy|NextURL/);
});
```

- [ ] **Step 2: Verify RED**

Run:

```bash
cd backend && go test ./internal/httpapi -run 'TestBackendDoesNotServeOrProxyWebRoutes' -v
cd .. && node --test scripts/project-boundaries.test.mjs
```

Expected: Go test fails because `/` is proxied/not found with the old handler, and the Node test fails on `NextURL` and `httputil`.

- [ ] **Step 3: Remove proxy state and make the backend root an explicit API 404**

Remove `net/http/httputil`, `net/url`, `Config.NextURL`, and `Server.nextProxy`. Build the handler as:

```go
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/api/", s.routeAPI)
	mux.HandleFunc("/uploads/shadowing", s.handleShadowingUpload)
	mux.HandleFunc("/uploads/shadowing/", s.handleShadowingUpload)
	mux.Handle(uploadsURLPrefix, uploadsHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	})
	return mux
}
```

Update `backend/cmd/api/main.go` to construct `httpapi.Config{DB: database}`, remove `NEXT_UPSTREAM_URL`, and log only:

```go
log.Printf("daily-speaking API listening on %s", addr)
```

Remove `NEXT_UPSTREAM_URL` from `scripts/smoke-api.mjs` and continue launching the Go process directly from `backend/`.

- [ ] **Step 4: Verify standalone backend GREEN**

Run:

```bash
cd backend && go test ./...
cd .. && node --test scripts/project-boundaries.test.mjs
DATABASE_URL=postgres://postgres:postgres@127.0.0.1:5432/daily_speaking npm run test:smoke
```

Expected: tests PASS; smoke calls port `3217` directly and no process attempts to contact Next.js.

- [ ] **Step 5: Commit the backend boundary**

```bash
git add backend/internal/httpapi/contract_test.go backend/internal/httpapi/server.go backend/cmd/api/main.go scripts/smoke-api.mjs scripts/project-boundaries.test.mjs
git commit -m "refactor: make backend a standalone API"
```

---

### Task 3: Add exact-origin CORS and explicit session-cookie configuration

**Files:**
- Create: `backend/internal/httpapi/cors.go`
- Create: `backend/internal/httpapi/cors_test.go`
- Create: `backend/internal/auth/cookie_test.go`
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/httpapi/*_test.go` where configured cookies are constructed

**Interfaces:**
- Consumes: `CORS_ALLOWED_ORIGINS`, `SESSION_COOKIE_SECURE`, `SESSION_COOKIE_SAME_SITE`, and `SESSION_COOKIE_DOMAIN`.
- Produces: `httpapi.ParseCORSConfig(string) (CORSConfig, error)`, `auth.CookieConfigFromEnv() (CookieConfig, error)`, exact credentialed CORS, and configured cookie creation without changing token values or database rows.

- [ ] **Step 1: Write failing CORS behavior tests**

Create `backend/internal/httpapi/cors_test.go` with these cases:

```go
func TestCORSAllowsConfiguredCredentialedOrigin(t *testing.T) {
	config, err := ParseCORSConfig("https://app.example.com,http://localhost:3218")
	if err != nil {
		t.Fatal(err)
	}
	handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/user/data", nil)
	request.Header.Set("Origin", "https://app.example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("unexpected allow origin %q", recorder.Header().Get("Access-Control-Allow-Origin"))
	}
	if recorder.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentialed CORS")
	}
}

func TestCORSRejectsUnsafeRequestFromUnknownOrigin(t *testing.T) {
	config, _ := ParseCORSConfig("https://app.example.com")
	handler := config.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("rejected origin must not receive CORS permission")
	}
}

func TestCORSRejectsWildcardAndOriginPaths(t *testing.T) {
	for _, raw := range []string{"*", "https://app.example.com/path"} {
		if _, err := ParseCORSConfig(raw); err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
	}
}
```

- [ ] **Step 2: Write failing cookie-configuration tests**

Create `backend/internal/auth/cookie_test.go`:

```go
func TestCookieConfigFromEnv(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("SESSION_COOKIE_SAME_SITE", "none")
	t.Setenv("SESSION_COOKIE_DOMAIN", ".example.com")

	config, err := CookieConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	cookie := NewSessionCookieWithConfig(config, "token", time.Unix(2_000_000_000, 0))
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("unexpected cookie %#v", cookie)
	}
	if cookie.Domain != ".example.com" || cookie.Path != "/" {
		t.Fatalf("unexpected scope %#v", cookie)
	}
}

func TestSameSiteNoneRequiresSecureCookie(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("SESSION_COOKIE_SAME_SITE", "none")
	if _, err := CookieConfigFromEnv(); err == nil {
		t.Fatal("expected SameSite=None without Secure to fail")
	}
}
```

- [ ] **Step 3: Run focused tests and verify RED**

Run: `cd backend && go test ./internal/httpapi ./internal/auth -run 'TestCORS|TestCookie|TestSameSite' -v`

Expected: compile failure because the new configuration types and functions do not exist.

- [ ] **Step 4: Implement CORS and configured cookies**

Implement `CORSConfig` as an immutable exact-origin set. `ParseCORSConfig` must trim entries, require `http`/`https`, reject credentials, paths, query strings, fragments, and `*`, and normalize only a trailing slash. `Wrap` must:

```go
func (c CORSConfig) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		_, allowed := c.allowedOrigins[origin]
		if origin != "" && !allowed && r.Method != http.MethodGet && r.Method != http.MethodHead {
			logging.ForRequest("api.cors", r).Warn("request.rejected", map[string]any{
				"status": http.StatusForbidden,
				"origin": origin,
				"reason": "origin_not_allowed",
			})
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Origin not allowed"})
			return
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if !allowed {
				logging.ForRequest("api.cors", r).Warn("request.rejected", map[string]any{
					"status": http.StatusForbidden,
					"origin": origin,
					"reason": "origin_not_allowed",
				})
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Origin not allowed"})
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Define `auth.CookieConfig` and keep compatibility helpers for integration-test fixtures:

```go
type CookieConfig struct {
	Secure   bool
	SameSite http.SameSite
	Domain   string
}

func NewSessionCookie(token string, expiresAt time.Time) *http.Cookie {
	return NewSessionCookieWithConfig(CookieConfig{SameSite: http.SameSiteLaxMode}, token, expiresAt)
}
```

Add `SessionCookie auth.CookieConfig` and `CORS CORSConfig` to `httpapi.Config`; store the cookie config on `Server`; normalize its zero value to `SameSite=Lax` inside `NewServer` so existing direct constructors remain compatible; use `NewSessionCookieWithConfig` and `ClearSessionCookieWithConfig` in register, login, session cleanup, and logout. Wrap the mux with `s.cors.Wrap(mux)`.

In `main.go`, parse both configs before connecting the server and fail with a concise configuration error. Empty `CORS_ALLOWED_ORIGINS` permits non-browser clients but grants no browser origin.

- [ ] **Step 5: Verify CORS, cookie compatibility, and full backend GREEN**

Run:

```bash
cd backend && gofmt -w internal/httpapi/cors.go internal/httpapi/cors_test.go internal/httpapi/server.go internal/auth/auth.go internal/auth/cookie_test.go cmd/api/main.go
go test ./internal/httpapi ./internal/auth -v
go test ./...
```

Expected: all tests PASS, including existing fixtures that still use `auth.NewSessionCookie`.

- [ ] **Step 6: Commit cross-origin session support**

```bash
git add backend/internal/httpapi backend/internal/auth backend/cmd/api/main.go
git commit -m "feat: support cross-origin web sessions"
```

---

### Task 4: Move OpenAPI into backend and serve Swagger

**Files:**
- Create: `backend/docs/embed.go`
- Move: `docs/api/openapi.json` to `backend/docs/openapi.json`
- Move and modify: `docs/api/swagger.html` to `backend/docs/swagger.html`
- Move and modify: `docs/api/README.md` to `backend/docs/README.md`
- Move and modify: `scripts/api-docs.test.mjs` to `backend/scripts/api-docs.test.mjs`
- Move and modify: `scripts/build-api-docs.mjs` to `backend/scripts/build-api-docs.mjs`
- Delete: `docs/api/openapi.spec.js`
- Create: `backend/internal/httpapi/docs_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/docs/openapi.json`
- Modify: `package.json`

**Interfaces:**
- Consumes: the existing OpenAPI 3.1 JSON route inventory.
- Produces: embedded `apidocs.OpenAPIJSON`, `apidocs.SwaggerHTML`, deterministic `node scripts/build-api-docs.mjs --check|--write`, `GET /openapi.json`, and `GET /docs` without web-container involvement.

- [ ] **Step 1: Write failing HTTP documentation tests**

Create `backend/internal/httpapi/docs_test.go`:

```go
func TestOpenAPIAndSwaggerAreServedByBackend(t *testing.T) {
	handler := NewServer(Config{}).Handler()
	cases := []struct {
		path        string
		contentType string
		bodyPart    string
	}{
		{"/openapi.json", "application/json", `"openapi": "3.1.0"`},
		{"/docs", "text/html", "SwaggerUIBundle"},
	}

	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, tc.path, nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.path, recorder.Code)
		}
		if !strings.Contains(recorder.Header().Get("Content-Type"), tc.contentType) {
			t.Fatalf("%s: wrong content type %q", tc.path, recorder.Header().Get("Content-Type"))
		}
		if !strings.Contains(recorder.Body.String(), tc.bodyPart) {
			t.Fatalf("%s: missing %q", tc.path, tc.bodyPart)
		}
	}
}
```

Update the moved Node contract test to require `/openapi.json`, `/docs`, all current API/upload routes, and the retained Feed paths. For every documented operation, assert a non-empty `summary`, at least one success response, the shared error response where applicable, `cookieAuth` on protected operations, declared path/query parameters, and a request body for every JSON or multipart mutation. Assert that the shared `User`, `Recording`, processing, suggestion, subscription, Feed post/reply, and `Error` schemas exist with examples for their externally visible fields.

Rewrite the moved build script as a dependency-free canonicalizer:

```js
import { readFileSync, writeFileSync } from "node:fs";

const mode = process.argv[2];
if (mode !== "--check" && mode !== "--write") {
  process.stderr.write("Usage: node scripts/build-api-docs.mjs --check|--write\n");
  process.exit(2);
}

const path = "docs/openapi.json";
const source = readFileSync(path, "utf8");
const document = JSON.parse(source);
const canonical = `${JSON.stringify(document, null, 2)}\n`;

if (mode === "--check" && source !== canonical) {
  process.stderr.write("OpenAPI formatting drifted. Run node scripts/build-api-docs.mjs --write\n");
  process.exit(1);
}
if (mode === "--write") {
  writeFileSync(path, canonical);
}
```

This preserves deterministic validation without generating a browser-side copy.

- [ ] **Step 2: Verify RED**

Run: `cd backend && go test ./internal/httpapi -run TestOpenAPIAndSwaggerAreServedByBackend -v`

Expected: FAIL with `404` for both routes.

- [ ] **Step 3: Embed and serve the backend-owned contract**

Create `backend/docs/embed.go`:

```go
package apidocs

import _ "embed"

//go:embed openapi.json
var OpenAPIJSON []byte

//go:embed swagger.html
var SwaggerHTML []byte
```

Change Swagger initialization to fetch the served artifact:

```html
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.ui = SwaggerUIBundle({
    url: "/openapi.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    persistAuthorization: true,
    displayRequestDuration: true,
    tryItOutEnabled: true,
    docExpansion: "list",
    defaultModelsExpandDepth: 2
  });
</script>
```

Register method-checked handlers before `/`:

```go
mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(apidocs.OpenAPIJSON)
})
mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(apidocs.SwaggerHTML)
})
```

Make `/` the first OpenAPI server so Swagger Try it out always targets the API origin that served `/docs`; retain `http://localhost:3219`, `https://localhost:3444`, and `https://api.example.com` as named examples. Keep `cookieAuth` and every Feed schema/operation.

Change the root orchestration aliases at the same time so `build:api-docs` becomes `cd backend && node scripts/build-api-docs.mjs --write` and `test:api-docs` becomes `cd backend && node scripts/build-api-docs.mjs --check && node --test scripts/api-docs.test.mjs`; no root command may point at a file after it moves.

- [ ] **Step 4: Verify documentation GREEN**

Run:

```bash
cd backend && gofmt -w docs/embed.go internal/httpapi/docs_test.go internal/httpapi/server.go
go test ./internal/httpapi -run 'TestOpenAPI|TestUnauthorizedAPIContract' -v
node scripts/build-api-docs.mjs --check
node --test scripts/api-docs.test.mjs
go test ./...
```

Expected: all tests PASS; the Node test parses `backend/docs/openapi.json` directly and has no generated-JavaScript drift check.

- [ ] **Step 5: Commit backend-owned Swagger**

```bash
git add backend/docs backend/scripts/api-docs.test.mjs backend/scripts/build-api-docs.mjs backend/internal/httpapi package.json docs/api scripts/api-docs.test.mjs scripts/build-api-docs.mjs
git commit -m "feat: serve backend Swagger documentation"
```

---

### Task 5: Add the runtime-configured web API client

**Files:**
- Create: `web/src/lib/apiConfig.ts`
- Create: `web/src/lib/apiClient.ts`
- Create: `web/scripts/api-client.test.mjs`
- Modify: `web/app/layout.tsx`
- Modify: `web/app/providers.tsx`
- Modify: `web/src/store/slices/appSlice.ts`
- Modify: `web/src/components/SpeakScreen.tsx`
- Modify: `web/src/components/DetailsScreen.tsx`
- Modify: `web/src/lib/data.ts`

**Interfaces:**
- Consumes: runtime `PUBLIC_API_BASE_URL` and browser `fetch`.
- Produces: `resolvePublicApiBaseUrl(value, environment)`, `createApiClient(baseURL, fetchImpl)`, `configureApiClient(baseURL)`, `apiFetch(path, init)`, `readApiJSON<T>(response)`, `ApiUnavailableError`, and `resolveApiAssetURL(value)`.

- [ ] **Step 1: Write failing API-client tests**

Create `web/scripts/api-client.test.mjs` using the existing TypeScript transpile helper and assert:

```js
test("production requires an absolute HTTP API URL", () => {
  assert.throws(
    () => apiConfig.resolvePublicApiBaseUrl(undefined, "production"),
    /PUBLIC_API_BASE_URL/,
  );
  assert.throws(
    () => apiConfig.resolvePublicApiBaseUrl("javascript:alert(1)", "production"),
    /http or https/,
  );
  assert.equal(
    apiConfig.resolvePublicApiBaseUrl("https://api.example.com/", "production"),
    "https://api.example.com",
  );
});

test("development defaults to the standalone API port", () => {
  assert.equal(
    apiConfig.resolvePublicApiBaseUrl(undefined, "development"),
    "http://localhost:3219",
  );
});

test("API requests preserve init and always include credentials", async () => {
  const calls = [];
  const controller = new AbortController();
  const body = new FormData();
  body.set("chunk", new Blob(["audio"]));
  const client = apiClient.createApiClient("https://api.example.com", async (url, init) => {
    calls.push({ url, init });
    return new Response(null, { status: 204 });
  });

  await client.fetch("/api/recording-sessions/demo/chunks", {
    method: "POST",
    body,
    signal: controller.signal,
  });

  assert.equal(calls[0].url, "https://api.example.com/api/recording-sessions/demo/chunks");
  assert.equal(calls[0].init.credentials, "include");
  assert.equal(calls[0].init.body, body);
  assert.equal(calls[0].init.signal, controller.signal);
});

test("network and malformed JSON failures become stable client errors", async () => {
  const unavailable = apiClient.createApiClient("https://api.example.com", async () => {
    throw new TypeError("fetch failed: internal hostname details");
  });
  await assert.rejects(
    () => unavailable.fetch("/healthz"),
    (error) => error instanceof apiClient.ApiUnavailableError
      && error.message === "The API is temporarily unavailable.",
  );

  const malformed = new Response("not-json", {
    status: 502,
    headers: { "Content-Type": "application/json" },
  });
  await assert.rejects(
    () => apiClient.readApiJSON(malformed),
    /The API returned an invalid response\./,
  );
});

test("only API-owned upload paths are resolved", () => {
  const client = apiClient.createApiClient("https://api.example.com", fetch);
  assert.equal(client.assetURL("/uploads/recordings/u/r.webm"), "https://api.example.com/uploads/recordings/u/r.webm");
  assert.equal(client.assetURL("data:audio/webm;base64,AAAA"), "data:audio/webm;base64,AAAA");
  assert.equal(client.assetURL("https://cdn.example/audio.mp3"), "https://cdn.example/audio.mp3");
});
```

- [ ] **Step 2: Verify RED**

Run: `cd web && node --test scripts/api-client.test.mjs`

Expected: FAIL because `apiConfig.ts` and `apiClient.ts` do not exist.

- [ ] **Step 3: Implement runtime validation and the client factory**

Implement pure runtime validation in `apiConfig.ts` and this public shape in `apiClient.ts`:

```ts
export type ApiClient = {
  fetch: (path: string, init?: RequestInit) => Promise<Response>;
  assetURL: (value: string | null) => string | null;
};

export const createApiClient = (
  baseURL: string,
  fetchImpl: typeof fetch = fetch,
): ApiClient => ({
  fetch(path, init = {}) {
    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    return fetchImpl(`${baseURL}${normalizedPath}`, {
      ...init,
      credentials: "include",
    });
  },
  assetURL(value) {
    if (!value || !value.startsWith("/uploads/")) {
      return value;
    }
    return `${baseURL}${value}`;
  },
});
```

Maintain one configured browser client and throw a clear initialization error if code calls `apiFetch` before `configureApiClient`.

Wrap non-abort network failures in `ApiUnavailableError("The API is temporarily unavailable.")` without exposing browser or host details. Preserve `AbortError` unchanged. Implement `readApiJSON<T>(response): Promise<T | null>` so an empty body returns `null`, valid JSON is returned as `T`, and malformed non-empty JSON throws `Error("The API returned an invalid response.")`. Replace local `.json().catch(() => null)` parsing in the migrated request paths with this helper while preserving each thunk's existing user-facing HTTP validation message.

Mark the root layout dynamic so runtime environment is read by the running container, resolve the value on the server, and pass it into `Providers`:

```tsx
export const dynamic = "force-dynamic";

export default function RootLayout({ children }: RootLayoutProps) {
  const apiBaseURL = resolvePublicApiBaseUrl(
    process.env.PUBLIC_API_BASE_URL,
    process.env.NODE_ENV,
  );
  return (
    <html lang="en">
      <body>
        <Providers apiBaseURL={apiBaseURL}>{children}</Providers>
      </body>
    </html>
  );
}
```

Configure the client in the same lazy initialization that creates the Redux store. Replace every raw application `fetch` in `appSlice.ts`, `SpeakScreen.tsx`, and `DetailsScreen.tsx` with `apiFetch` (the Feed-specific detail requests are removed in Task 6). Resolve parsed recording `audioDataUrl`, `photoDataUrl`, and `shadowingAudioUrl` with `resolveApiAssetURL`; data URLs and external URLs remain unchanged.

- [ ] **Step 4: Add a source guard against bypassing the client**

Append to `web/scripts/api-client.test.mjs`:

```js
test("application network calls use the shared API client", () => {
  for (const path of [
    "src/store/slices/appSlice.ts",
    "src/components/SpeakScreen.tsx",
    "src/components/DetailsScreen.tsx",
  ]) {
    const source = readFileSync(path, "utf8");
    assert.doesNotMatch(source, /\bfetch\s*\(/, `${path} bypasses apiFetch`);
  }
});
```

- [ ] **Step 5: Verify API client GREEN**

Run:

```bash
cd web
node --test scripts/api-client.test.mjs
npm run typecheck
npm run lint
npm test
npm run build
```

Expected: all commands PASS; the production build succeeds with an explicit test value such as `PUBLIC_API_BASE_URL=http://localhost:3219` if the build evaluates runtime layout validation.

- [ ] **Step 6: Commit the direct API client**

```bash
git add web/app web/src web/scripts/api-client.test.mjs
git commit -m "feat: call standalone API from web"
```

---

### Task 6: Remove Feed and legacy sharing from the web project

**Files:**
- Create: `web/scripts/feed-removal.test.mjs`
- Delete: `web/src/components/FeedScreen.tsx`
- Delete: `web/src/components/FeedThreadScreen.tsx`
- Delete: `web/src/components/FeedReactionBar.tsx`
- Delete: `web/src/components/ShareModal.tsx`
- Delete: `web/src/components/ShareScreen.tsx`
- Modify: `web/src/components/AppShell.tsx`
- Modify: `web/src/components/DetailsScreen.tsx`
- Modify: `web/src/store/slices/appSlice.ts`
- Modify: `web/src/lib/data.ts`
- Modify: `web/src/lib/recordingDeletion.ts`
- Modify: `web/src/lib/utils.ts`
- Modify: `web/scripts/recording-deletion.test.mjs`
- Modify: `web/app/globals.css`

**Interfaces:**
- Consumes: the existing recording/history/profile client features and backend Feed API.
- Produces: a web client with no Feed UI/state/network calls while backend route source and `backend/docs/openapi.json` still contain Feed.

- [ ] **Step 1: Write the failing client-removal test**

Create `web/scripts/feed-removal.test.mjs`:

```js
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

test("Feed UI and publication are absent from web", () => {
  for (const path of [
    "src/components/FeedScreen.tsx",
    "src/components/FeedThreadScreen.tsx",
    "src/components/FeedReactionBar.tsx",
    "src/components/ShareModal.tsx",
    "src/components/ShareScreen.tsx",
  ]) {
    assert.equal(existsSync(path), false, `${path} should be deleted`);
  }

  const webSource = [
    readFileSync("src/components/AppShell.tsx", "utf8"),
    readFileSync("src/components/DetailsScreen.tsx", "utf8"),
    readFileSync("src/store/slices/appSlice.ts", "utf8"),
  ].join("\n");
  assert.doesNotMatch(webSource, /Publish to Feed|fetchFeedPosts|publishRecordingToFeed|currentFeedPost/);
});

test("backend Feed API and documentation remain", () => {
  const server = readFileSync("../backend/internal/httpapi/server.go", "utf8");
  const openapi = JSON.parse(readFileSync("../backend/docs/openapi.json", "utf8"));
  assert.match(server, /\/api\/feed\/posts/);
  assert.ok(openapi.paths["/api/feed/posts"]);
  assert.ok(openapi.paths["/api/feed/posts/{postId}/replies"]);
});
```

- [ ] **Step 2: Verify RED**

Run: `cd web && node --test scripts/feed-removal.test.mjs`

Expected: FAIL because all five components and Feed Redux symbols still exist.

- [ ] **Step 3: Delete Feed UI and remove its state graph**

Delete the five components. Remove these categories from `appSlice.ts`:

```text
FeedPost, FeedReply, FeedReaction, FeedReactionSummary imports and parsers
feedPosts/feedThread/currentFeed* state
feedPublish/feedReply/feedReaction status and errors
fetchFeedPosts, fetchFeedThread, publishRecordingToFeed
createFeedReply, reactToFeedPost, reactToFeedReply
openFeedThread, backToFeed, openShareModal, closeShareModal
setShareAction, openSharePreview, setCopyMessage
clearFeedState, clearFeedThreadState, upsertFeedPost
all matching extraReducers and exported actions
```

Remove `Feed*` types and reaction constants from `data.ts`. Reduce recording deletion helpers to:

```ts
type RecordingLike = { id: string };

export const filterDeletedRecordings = <R extends RecordingLike>(
  recordings: R[],
  recordingIds: string[],
): R[] => {
  const deletedIds = new Set(recordingIds);
  return recordings.filter((recording) => !deletedIds.has(recording.id));
};

export const removeRecording = <R extends RecordingLike>(
  recordings: R[],
  recordingId: string,
): R[] => filterDeletedRecordings(recordings, [recordingId]);
```

Update the deletion reducer to remove only the client recording. Keep backend cascade behavior untouched.

In `DetailsScreen.tsx`, remove Feed fetches, publication/comment state, `ShareModal`, `copyMessage`, and the bottom Feed block. Change deletion copy to `This permanently deletes the recording and its audio files.` Remove unused share-link helpers and obsolete Feed CSS selectors.

- [ ] **Step 4: Update recording deletion tests and verify GREEN**

Replace the Feed-coupled assertion with:

```js
test("removing a recording keeps unrelated recordings", () => {
  assert.deepEqual(
    deletion.removeRecording(
      [{ id: "recording-1" }, { id: "recording-2" }],
      "recording-1",
    ),
    [{ id: "recording-2" }],
  );
});
```

Run:

```bash
cd web
node --test scripts/feed-removal.test.mjs scripts/recording-deletion.test.mjs
npm run typecheck
npm run lint
npm test
cd ../backend && go test ./...
node --test scripts/api-docs.test.mjs
```

Expected: all commands PASS and backend Feed tests/docs remain unchanged.

- [ ] **Step 5: Commit the web-only Feed removal**

```bash
git add web backend/docs/openapi.json
git commit -m "refactor: remove unfinished Feed from web"
```

---

### Task 7: Add route primitives, shared shell, and authenticated guards

**Files:**
- Create: `web/src/lib/routes.ts`
- Create: `web/scripts/routes.test.mjs`
- Create: `web/src/components/ProtectedRoute.tsx`
- Create: `web/src/components/AuthRoute.tsx`
- Modify: `web/src/components/AppShell.tsx`
- Modify: `web/app/layout.tsx`
- Modify: `web/app/page.tsx`
- Create: `web/app/speak/page.tsx`
- Create: `web/app/auth/page.tsx`
- Create: `web/app/history/page.tsx`
- Create: `web/app/history/[recordingId]/page.tsx`
- Create: `web/app/profile/page.tsx`
- Create: `web/app/profile/subscription/page.tsx`
- Create: `web/app/profile/english-level/page.tsx`
- Create: `web/app/profile/interests/page.tsx`
- Modify: `web/src/store/slices/appSlice.ts`

**Interfaces:**
- Consumes: session state (`authInitialized`, `authStatus`, `isAuthenticated`) and route values from Next.js.
- Produces: `safeReturnTo(value)`, `parseHistoryDate(value)`, `protectedRouteDestination(authInitialized, isAuthenticated, returnTo)`, route pages, `ProtectedRoute`, and an `AppShell` that renders `children` rather than Redux-selected screens.

- [ ] **Step 1: Write failing pure route tests**

Create `web/scripts/routes.test.mjs`:

```js
test("safeReturnTo accepts only known internal routes", () => {
  for (const path of [
    "/speak",
    "/history",
    "/history?date=2026-09-21",
    "/history/recording-123",
    "/profile",
    "/profile/subscription",
    "/profile/english-level",
    "/profile/interests",
  ]) {
    assert.equal(routes.safeReturnTo(path), path);
  }
  for (const value of [
    "https://evil.example",
    "//evil.example",
    "/feed",
    "/history/../profile",
    "/history/%",
    "/history?date=2026-02-30",
    "/history?date=2026-09-21&next=https://evil.example",
    "javascript:alert(1)",
  ]) {
    assert.equal(routes.safeReturnTo(value), "/speak");
  }
});

test("history date accepts only real YYYY-MM-DD dates", () => {
  assert.equal(routes.parseHistoryDate("2026-09-21"), "2026-09-21");
  assert.equal(routes.parseHistoryDate("2026-02-30"), null);
  assert.equal(routes.parseHistoryDate("21-09-2026"), null);
});

test("protected routes wait for session restoration before redirecting", () => {
  assert.equal(
    routes.protectedRouteDestination(false, false, "/history"),
    null,
  );
  assert.equal(
    routes.protectedRouteDestination(true, true, "/history"),
    null,
  );
  assert.equal(
    routes.protectedRouteDestination(true, false, "/history/recording-123"),
    "/auth?returnTo=%2Fhistory%2Frecording-123",
  );
  assert.equal(
    routes.protectedRouteDestination(true, false, "//evil.example"),
    "/auth?returnTo=%2Fspeak",
  );
});
```

- [ ] **Step 2: Verify RED**

Run: `cd web && node --test scripts/routes.test.mjs`

Expected: FAIL because `src/lib/routes.ts` does not exist.

- [ ] **Step 3: Implement strict route parsing**

Define literal routes, a single validated History query, and encoded recording IDs:

```ts
const STATIC_RETURN_ROUTES = new Set([
  "/speak",
  "/history",
  "/profile",
  "/profile/subscription",
  "/profile/english-level",
  "/profile/interests",
]);

export const recordingPath = (recordingId: string): string =>
  `/history/${encodeURIComponent(recordingId)}`;

export const safeReturnTo = (value: unknown): string => {
  if (typeof value !== "string" || !value.startsWith("/") || value.startsWith("//")) {
    return "/speak";
  }
  const [path, rawQuery = "", ...extra] = value.split("?");
  if (extra.length > 0 || value.includes("#")) {
    return "/speak";
  }
  if (STATIC_RETURN_ROUTES.has(path) && rawQuery === "") {
    return path;
  }
  if (path === "/history" && rawQuery !== "") {
    const params = new URLSearchParams(rawQuery);
    const date = params.get("date");
    if (params.size === 1 && date && parseHistoryDate(date)) {
      return `/history?date=${encodeURIComponent(date)}`;
    }
  }
  return rawQuery === "" && /^\/history\/[A-Za-z0-9-]+$/.test(path)
    ? path
    : "/speak";
};
```

`parseHistoryDate` must round-trip the UTC year/month/day and reject invalid calendar dates.

Implement `protectedRouteDestination` as a pure helper that returns `null` while authentication is uninitialized or authenticated, and otherwise returns `/auth?returnTo=${encodeURIComponent(safeReturnTo(returnTo))}`. `ProtectedRoute` must use this helper so the tested decision is the component's decision.

- [ ] **Step 4: Replace conditional screen rendering with route pages**

Change `AppShell` to accept `children: ReactNode`, run only session/user bootstrap effects, render `Link` elements for Speak and authenticated History, derive active state from `usePathname`, link the email to `/profile`, and route logout to `/speak` after dispatch.

Wrap `AppShell` around `children` in the root layout. Implement root redirect:

```tsx
import { redirect } from "next/navigation";

export default function Page() {
  redirect("/speak");
}
```

Each protected page wraps its screen in `<ProtectedRoute returnTo="...">`. `ProtectedRoute` renders a loading state until `authInitialized`; when redirecting in the browser, it passes `window.location.pathname + window.location.search` through `protectedRouteDestination` so a valid History date query survives auth, while the prop is its deterministic fallback. It only calls `router.replace(destination)` when unauthenticated. `AuthRoute` reads `returnTo` with `useSearchParams`, validates it through `safeReturnTo`, and supplies it to `AuthScreen`; `app/auth/page.tsx` wraps `AuthRoute` in `Suspense` so the production App Router build accepts the search-param hook.

Remove `ScreenName`, `TabName`, `currentScreen`, `activeTab`, `screenBeforeAuth`, and the navigation-only Redux reducers/actions. Reduce the temporary `openAuthFlow` and `cancelAuth` reducers to authentication-form/domain cleanup only; Task 8 replaces their remaining navigation callers with routes. Keep `currentRecordingId` as selected domain data for playback during Task 8.

Keep this intermediate commit buildable: the history/details/profile route pages initially render the existing screens without new props, and all three profile subsection routes may temporarily render the existing `ProfileScreen`. Task 8 introduces `recordingId` and `section` props and then gives each URL its final behavior.

- [ ] **Step 5: Add a route-source structure test and verify GREEN**

Append to `routes.test.mjs`:

```js
test("App Router owns every supported screen", () => {
  for (const path of [
    "app/speak/page.tsx",
    "app/auth/page.tsx",
    "app/history/page.tsx",
    "app/history/[recordingId]/page.tsx",
    "app/profile/page.tsx",
    "app/profile/subscription/page.tsx",
    "app/profile/english-level/page.tsx",
    "app/profile/interests/page.tsx",
  ]) {
    assert.equal(existsSync(path), true, `missing ${path}`);
  }
  const appSlice = readFileSync("src/store/slices/appSlice.ts", "utf8");
  assert.doesNotMatch(appSlice, /currentScreen|activeTab|navigateToTab|screenBeforeAuth/);
});
```

Run:

```bash
cd web
node --test scripts/routes.test.mjs
npm run typecheck
npm run lint
npm run build
```

Expected: all routes build and tests PASS.

- [ ] **Step 6: Commit route ownership**

```bash
git add web/app web/src/components/AppShell.tsx web/src/components/ProtectedRoute.tsx web/src/components/AuthRoute.tsx web/src/lib/routes.ts web/src/store/slices/appSlice.ts web/scripts/routes.test.mjs
git commit -m "feat: add persistent Next.js routes"
```

---

### Task 8: Connect authentication, history, details, profile, and optimistic saves to routes

**Files:**
- Create: `web/scripts/route-flows.test.mjs`
- Modify: `web/src/components/AuthScreen.tsx`
- Modify: `web/src/components/SpeakScreen.tsx`
- Modify: `web/src/components/HistoryScreen.tsx`
- Modify: `web/src/components/DetailsScreen.tsx`
- Modify: `web/src/components/ProfileScreen.tsx`
- Modify: `web/src/components/InterestsScreen.tsx`
- Modify: `web/src/store/slices/appSlice.ts`
- Modify: `web/app/auth/page.tsx`
- Modify: `web/app/history/page.tsx`
- Modify: `web/app/history/[recordingId]/page.tsx`
- Modify: `web/app/profile/*.tsx`

**Interfaces:**
- Consumes: `recordingPath`, `safeReturnTo`, `parseHistoryDate`, thunk `.unwrap()` results, and the route page parameters.
- Produces: reloadable recording-detail URLs, query-backed history dates, route-backed profile sections, safe post-auth return, and deterministic optimistic-save navigation.

- [ ] **Step 1: Write failing route-flow source tests**

Create `web/scripts/route-flows.test.mjs`:

```js
test("screen components navigate with Next router instead of Redux navigation", () => {
  const history = readFileSync("src/components/HistoryScreen.tsx", "utf8");
  const details = readFileSync("src/components/DetailsScreen.tsx", "utf8");
  const profile = readFileSync("src/components/ProfileScreen.tsx", "utf8");
  assert.match(history, /recordingPath\(recording\.id\)/);
  assert.doesNotMatch(history, /openDetails/);
  assert.match(details, /router\.(push|replace)\("\/history"\)/);
  assert.doesNotMatch(details, /backToHistory/);
  assert.match(profile, /href="\/profile\/subscription"/);
  assert.match(profile, /href="\/profile\/english-level"/);
});

test("optimistic save replaces the local route with the server id", () => {
  const speak = readFileSync("src/components/SpeakScreen.tsx", "utf8");
  assert.match(speak, /router\.push\(recordingPath\(draft\.localRecordingId/);
  assert.match(speak, /router\.replace\(recordingPath\(result\.recording\.id\)\)/);
  assert.match(speak, /router\.replace\("\/history"\)/);
});
```

- [ ] **Step 2: Verify RED**

Run: `cd web && node --test scripts/route-flows.test.mjs`

Expected: FAIL because screens still dispatch Redux navigation actions.

- [ ] **Step 3: Route authentication and pending post-auth saves**

Give `AuthScreen` validated route callbacks. On successful sign-in/register:

```ts
const user = await dispatch(signIn()).unwrap();
if (!user) return;
if (pendingSaveAfterAuth) {
  const result = await dispatch(saveRecording()).unwrap();
  router.replace(recordingPath(result.recording.id));
  return;
}
router.replace(returnTo);
```

Catch rejected thunks without navigating, leaving their Redux error visible. Cancel clears auth drafts and routes to `/speak`. Remove AppShell's old post-auth save effect so the save happens exactly once.

- [ ] **Step 4: Route optimistic recording saves**

In `SpeakScreen`, inject `useRouter`. For an authenticated save, require `draft.localRecordingId`, dispatch `showBackgroundRecordingSave(draft)`, push the local detail URL immediately, then replace it after either the primary upload-session save or fallback data-URL save succeeds:

```ts
router.push(recordingPath(draft.localRecordingId));
try {
  if (finalUpload) await finalUpload;
  const result = await dispatch(saveRecording(draft)).unwrap();
  router.replace(recordingPath(result.recording.id));
} catch {
  try {
    const result = await dispatch(
      saveRecording({ ...draft, recordingUploadSessionId: null }),
    ).unwrap();
    router.replace(recordingPath(result.recording.id));
  } catch {
    router.replace("/history");
  }
}
```

For a guest, keep `openAuthForSave` as domain state and push `/auth?returnTo=%2Fspeak`.

- [ ] **Step 5: Route history filters, details, and profile subsections**

Make `HistoryScreen` consume `selectedDate` parsed from `useSearchParams`. Date selection uses `router.replace(`/history?date=${dateString}`)` and clearing uses `router.replace("/history")`. Render recording cards as `Link href={recordingPath(recording.id)}`. Remove `selectedDate`, `setSelectedDate`, and `clearSelectedDate` from Redux after the URL owns the filter.

Make the dynamic page await and decode its Next.js 15 route params, then pass `recordingId` to `DetailsScreen`. Details synchronizes `currentRecordingId` as domain selection, fetches the recording by route ID when not present, links back to `/history`, and awaits deletion before `router.replace("/history")`. A `404` or owner rejection renders a stable error and History link.

Make `ProfileScreen` accept `section: "home" | "subscription" | "english-level"`; replace local `view` transitions with links among the three routes. `InterestsScreen` links back to `/profile`. Each profile page passes one literal section.

- [ ] **Step 6: Verify all route flows GREEN**

Run:

```bash
cd web
node --test scripts/routes.test.mjs scripts/route-flows.test.mjs scripts/recording-upload-flow.test.mjs
npm run typecheck
npm run lint
npm test
PUBLIC_API_BASE_URL=http://localhost:3219 npm run build
```

Expected: all commands PASS; Next build lists every intended route and no `/feed` route.

- [ ] **Step 7: Commit routed feature flows**

```bash
git add web/app web/src web/scripts/route-flows.test.mjs
git commit -m "refactor: connect application flows to routes"
```

---

### Task 9: Build and run independent web and backend containers

**Files:**
- Modify: `web/Dockerfile`
- Modify: `backend/Dockerfile`
- Create: `web/app/web-healthz/route.ts`
- Modify: `docker-compose.yml`
- Delete: `Dockerfile`
- Delete: `docker-entrypoint.sh`
- Modify: `web/.dockerignore`
- Modify: `backend/.dockerignore`
- Modify: `scripts/ci-workflows.test.mjs`

**Interfaces:**
- Consumes: `PUBLIC_API_BASE_URL`, backend runtime variables, external `UPLOADS_HOST_DIR`, and the PostgreSQL service.
- Produces: `web` and `backend` images with separate build contexts, service health endpoints, and no combined entrypoint.

- [ ] **Step 1: Extend failing infrastructure tests**

Add assertions to `scripts/ci-workflows.test.mjs`:

```js
const compose = readFileSync("docker-compose.yml", "utf8");
const webDockerfile = readFileSync("web/Dockerfile", "utf8");
const backendDockerfile = readFileSync("backend/Dockerfile", "utf8");

test("Compose runs independent web and backend images", () => {
  assert.match(compose, /\n  web:\n/);
  assert.match(compose, /context:\s*\.\/web/);
  assert.match(compose, /\n  backend:\n/);
  assert.match(compose, /context:\s*\.\/backend/);
  assert.match(compose, /\$\{APP_PORT:-3218\}:3000/);
  assert.match(compose, /\$\{API_PORT:-3219\}:3000/);
  assert.match(compose, /\$\{UPLOADS_HOST_DIR:-\.\/\.data\/uploads\}:\/app\/uploads/);
  assert.match(compose, /postgres_data:\/var\/lib\/postgresql\/data/);
  assert.equal(existsSync("Dockerfile"), false);
  assert.equal(existsSync("docker-entrypoint.sh"), false);
});

test("application images contain only their own runtimes", () => {
  assert.doesNotMatch(webDockerfile, /golang|daily-speaking-api|whisper|ffmpeg/);
  assert.doesNotMatch(backendDockerfile, /node:|next-build|server\.js/);
  assert.match(backendDockerfile, /openai-whisper/);
});
```

- [ ] **Step 2: Verify RED**

Run: `node --test scripts/ci-workflows.test.mjs`

Expected: FAIL because Compose still defines `app` and the combined root Dockerfile still exists.

- [ ] **Step 3: Finish the web standalone image and health route**

Implement `GET /web-healthz`:

```ts
export function GET() {
  return Response.json({ ok: true, service: "web" });
}
```

Use a multi-stage standalone image:

```dockerfile
FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS build
WORKDIR /app
COPY . .
RUN mkdir -p public && npm run build

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
EXPOSE 3000
CMD ["node", "server.js"]
```

- [ ] **Step 4: Finish the backend-only image**

Use the current Whisper runtime without Node:

```dockerfile
FROM golang:1.26.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-api ./cmd/api

FROM debian:bookworm-slim AS runtime
WORKDIR /app
ENV APP_ADDR=:3000
ENV UPLOADS_DIR=/app/uploads
ENV WHISPER_BACKEND=openai
ENV WHISPER_PYTHON_BIN=/opt/whisper/bin/python
ENV WHISPER_OPENAI_MODEL=base
ENV WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
ENV WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
ENV WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
ENV WHISPER_OPENAI_DEVICE=cpu
ENV WHISPER_OPENAI_FP16=false
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg python3 python3-venv \
  && python3 -m venv /opt/whisper \
  && /opt/whisper/bin/pip install --no-cache-dir -U pip openai-whisper \
  && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/daily-speaking-api ./daily-speaking-api
RUN mkdir -p /app/uploads /app/tools/whisper/openai-models /app/tools/whisper/cache
EXPOSE 3000
CMD ["./daily-speaking-api"]
```

- [ ] **Step 5: Split Compose service ownership**

Define `web` with `context: ./web`, `PUBLIC_API_BASE_URL`, port `3218`, and a `/web-healthz` health check. Define `backend` with `context: ./backend`, port `3219`, the existing DB/AI/Whisper/Cartesia variables, explicit session-cookie/CORS variables, uploads mount, tools mount, and `/healthz` health check. Only backend depends on PostgreSQL.

Use these defaults:

```yaml
web:
  environment:
    PUBLIC_API_BASE_URL: ${PUBLIC_API_BASE_URL:-http://localhost:3219}
  ports:
    - "0.0.0.0:${APP_PORT:-3218}:3000"

backend:
  environment:
    CORS_ALLOWED_ORIGINS: ${CORS_ALLOWED_ORIGINS:-http://localhost:3218,http://127.0.0.1:3218}
    SESSION_COOKIE_SECURE: ${SESSION_COOKIE_SECURE:-false}
    SESSION_COOKIE_SAME_SITE: ${SESSION_COOKIE_SAME_SITE:-lax}
  ports:
    - "0.0.0.0:${API_PORT:-3219}:3000"
  volumes:
    - ${UPLOADS_HOST_DIR:-./.data/uploads}:/app/uploads
    - ${WHISPER_TOOLS_HOST_DIR:-./backend/tools}:/app/tools
```

Remove the combined root Dockerfile and entrypoint.

- [ ] **Step 6: Verify images and Compose GREEN**

Run:

```bash
node --test scripts/ci-workflows.test.mjs
docker compose config
docker compose build web backend
```

Expected: tests PASS, Compose resolves both build contexts, and both images build without copying the other project.

- [ ] **Step 7: Commit independent images**

```bash
git add web/Dockerfile web/.dockerignore web/app/web-healthz/route.ts backend/Dockerfile backend/.dockerignore docker-compose.yml scripts/ci-workflows.test.mjs Dockerfile docker-entrypoint.sh
git commit -m "build: split web and backend containers"
```

---

### Task 10: Route separate HTTP/HTTPS services through deployment infrastructure

**Files:**
- Modify: `scripts/docker-lan.mjs`
- Modify: `scripts/docker-lan.test.mjs`
- Modify: `scripts/setup-lan-https-proxy.ps1`
- Modify: `scripts/lan-https-proxy.test.mjs`
- Modify: `docker-compose.yml`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `APP_PORT`, `API_PORT`, `HTTPS_PORT`, `API_HTTPS_PORT`, detected LAN IP, and Caddy.
- Produces: independent public web/API URLs, runtime web API configuration, exact CORS origins, two Caddy sites, and firewall setup for both HTTPS ports.

- [ ] **Step 1: Write failing LAN URL and Caddy tests**

Extend `scripts/docker-lan.test.mjs`:

```js
test("LAN summary reports independent web and API URLs", () => {
  const summary = formatLanSummary({
    webPort: "3218",
    apiPort: "3219",
    lanAddresses: ["192.168.1.42"],
  });
  assert.match(summary, /Web:\s+http:\/\/192\.168\.1\.42:3218/);
  assert.match(summary, /API:\s+http:\/\/192\.168\.1\.42:3219/);
  assert.match(summary, /Swagger:\s+http:\/\/192\.168\.1\.42:3219\/docs/);
});
```

Change `scripts/lan-https-proxy.test.mjs` to require:

```js
assert.match(script, /\[int\]\$ApiHttpsPort/);
assert.match(script, /reverse_proxy web:3000/);
assert.match(script, /reverse_proxy backend:3000/);
assert.match(script, /PUBLIC_API_BASE_URL/);
assert.match(script, /CORS_ALLOWED_ORIGINS/);
assert.match(script, /docker compose up --build -d web backend postgres lan-https/);
assert.match(compose, /\$\{API_HTTPS_PORT:-3444\}:3444/);
```

- [ ] **Step 2: Verify RED**

Run: `node --test scripts/docker-lan.test.mjs scripts/lan-https-proxy.test.mjs`

Expected: FAIL because only one application URL, Caddy upstream, and HTTPS port exist.

- [ ] **Step 3: Configure LAN HTTP for direct API traffic**

Make `docker-lan.mjs` select the preferred LAN address, set:

```js
const publicApiBaseURL = `http://${hostAddress}:${apiPort}`;
const corsAllowedOrigins = [
  `http://${hostAddress}:${webPort}`,
  `http://localhost:${webPort}`,
  `http://127.0.0.1:${webPort}`,
].join(",");
```

Pass `PUBLIC_API_BASE_URL`, `CORS_ALLOWED_ORIGINS`, `APP_PORT`, and `API_PORT` into `docker compose up --build -d web backend postgres`. Print web, API health, Swagger, and microphone HTTPS guidance separately.

- [ ] **Step 4: Generate two HTTPS Caddy sites**

Add `AppPort` defaulting to `3218`, `ApiPort` defaulting to `3219`, and `ApiHttpsPort` defaulting to `3444`; create firewall rules for both HTTPS ports, and generate:

```caddyfile
https://${HostIp}:${HttpsPort}, https://localhost:${HttpsPort}, https://127.0.0.1:${HttpsPort} {
  tls /certs/daily-speaking.pem /certs/daily-speaking-key.pem
  reverse_proxy web:3000
}

https://${HostIp}:${ApiHttpsPort}, https://localhost:${ApiHttpsPort}, https://127.0.0.1:${ApiHttpsPort} {
  tls /certs/daily-speaking.pem /certs/daily-speaking-key.pem
  reverse_proxy backend:3000
}
```

Before Compose starts, set:

```powershell
$env:APP_PORT = "$AppPort"
$env:API_PORT = "$ApiPort"
$env:PUBLIC_API_BASE_URL = "https://${HostIp}:${ApiHttpsPort}"
$env:CORS_ALLOWED_ORIGINS = "https://${HostIp}:${HttpsPort},https://localhost:${HttpsPort},https://127.0.0.1:${HttpsPort},http://${HostIp}:${AppPort},http://localhost:${AppPort},http://127.0.0.1:${AppPort}"
$env:SESSION_COOKIE_SECURE = "true"
$env:SESSION_COOKIE_SAME_SITE = "lax"
```

Expose both Caddy ports in Compose and start `web backend postgres lan-https`.

- [ ] **Step 5: Verify deployment-script GREEN**

Run:

```bash
node --test scripts/docker-lan.test.mjs scripts/lan-https-proxy.test.mjs
docker compose config
```

Expected: tests PASS; resolved Compose contains web `3218`, API `3219`, web HTTPS `3443`, and API HTTPS `3444`.

- [ ] **Step 6: Commit direct-origin infrastructure**

```bash
git add scripts/docker-lan.mjs scripts/docker-lan.test.mjs scripts/setup-lan-https-proxy.ps1 scripts/lan-https-proxy.test.mjs docker-compose.yml .env.example
git commit -m "build: expose independent web and API origins"
```

---

### Task 11: Update CI/CD and add separate-service smoke coverage

**Files:**
- Create: `scripts/smoke-stack.mjs`
- Create: `scripts/stack-smoke.test.mjs`
- Modify: `scripts/smoke-api.mjs`
- Modify: `scripts/ci-workflows.test.mjs`
- Modify: `.github/workflows/quality-gates.yml`
- Modify: `.github/workflows/deploy-local.yml`
- Modify: `package.json`

**Interfaces:**
- Consumes: independent project commands, Compose service names, web/API public ports, existing GitHub secrets/variables, and backend Swagger endpoints.
- Produces: independently cached/tested/built applications and a deployment gate that fails when either public service, CORS/session behavior, uploads, or Swagger is broken.

- [ ] **Step 1: Write failing workflow contract tests**

Extend `scripts/ci-workflows.test.mjs` with exact requirements:

```js
test("quality workflow installs and builds web independently", () => {
  assert.match(qualityWorkflow, /cache-dependency-path:\s+web\/package-lock\.json/);
  assert.match(qualityWorkflow, /npm ci --prefix web/);
  assert.match(qualityWorkflow, /npm run quality --prefix web/);
  assert.match(qualityWorkflow, /cd backend && go test \.\/\.\.\./);
  assert.match(qualityWorkflow, /docker compose build web backend/);
});

test("Windows deploy checks both services", () => {
  assert.match(deployWorkflow, /API_PORT:/);
  assert.match(deployWorkflow, /API_HTTPS_PORT:/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 web/);
  assert.match(deployWorkflow, /docker compose logs --tail 120 backend/);
  assert.match(deployWorkflow, /docker compose exec -T backend/);
  assert.match(deployWorkflow, /node scripts\/smoke-stack\.mjs/);
});
```

- [ ] **Step 2: Verify RED**

Run: `node --test scripts/ci-workflows.test.mjs`

Expected: FAIL because workflows still install the root client and refer to the combined `app` service.

- [ ] **Step 3: Add direct API and combined-stack smoke checks**

Keep `smoke-api.mjs` backend-only and remove every Next variable. Add `smoke-stack.mjs` that reads:

```js
const webBaseURL = process.env.WEB_BASE_URL ?? "http://127.0.0.1:3218";
const apiBaseURL = process.env.API_BASE_URL ?? "http://127.0.0.1:3219";
const webOrigin = new URL(webBaseURL).origin;
```

It must verify:

```text
GET  web /web-healthz -> 200 and service=web
GET  web /speak -> 200
GET  API /healthz -> 200
GET  API /openapi.json -> 200 and OpenAPI 3.1.0
GET  API /docs -> 200 and SwaggerUIBundle
OPTIONS API /api/auth/session with Origin=webOrigin -> 204 and credentialed CORS headers
POST API /api/auth/register with Origin=webOrigin -> 201 plus session cookie
GET  API /api/auth/session with Origin and Cookie -> 200
GET  API /api/user/data with Origin and Cookie -> 200
POST API /api/user/recordings with Origin, Cookie, and a tiny valid data-URL audio payload -> 201
GET  API recording.audioDataUrl with Origin and Cookie -> 200 and non-empty audio bytes
DELETE API /api/recordings/{id} with Origin and Cookie -> 200 cleanup
POST API /api/auth/logout with Origin and Cookie -> 200
```

Use topic `Stack smoke`, duration `1`, practice type `free_talk`, a current ISO timestamp, and `data:audio/webm;base64,AAAA` for the disposable recording. Resolve its returned `/uploads/...` value against `apiBaseURL`, delete it before logout, use a unique smoke email, never print the cookie, and report only status/body excerpts on failure. Add unit coverage for URL selection, API-owned upload URL resolution, and cookie-header extraction in `stack-smoke.test.mjs`.

- [ ] **Step 4: Split quality workflow commands and cache**

Configure setup-node with `cache-dependency-path: web/package-lock.json`, run `npm ci --prefix web`, then explicit web, backend, API-doc, infrastructure tests, backend smoke, and `docker compose build web backend`. Supply `PUBLIC_API_BASE_URL=http://localhost:3219` to `next build` without baking it into the Docker image.

- [ ] **Step 5: Update Windows deployment atomically**

Add defaults `API_PORT=3219` and `API_HTTPS_PORT=3444`. Keep Cartesia validation and the external Windows uploads path. Run the updated HTTPS setup script, then `smoke-stack.mjs` against HTTP loopback endpoints. Check HTTPS endpoints with the installed mkcert trust. Run Whisper and Cartesia verification in `backend`, not `app`. On failure print `docker compose ps` and separate last-120-line logs for `web`, `backend`, and `lan-https`.

- [ ] **Step 6: Verify workflow and smoke GREEN**

Run:

```bash
node --test scripts/ci-workflows.test.mjs scripts/stack-smoke.test.mjs
npm run quality
docker compose up --build -d web backend postgres
node scripts/smoke-stack.mjs
docker compose down
```

Expected: all tests and smoke checks PASS; shutdown retains the named PostgreSQL volume and external upload files.

- [ ] **Step 7: Commit CI/CD migration**

```bash
git add scripts .github/workflows package.json
git commit -m "ci: deploy separate web and backend services"
```

---

### Task 12: Update operations documentation and record follow-up epics

**Files:**
- Create: `web/README.md`
- Create: `web/.env.example`
- Create: `backend/README.md`
- Create: `backend/.env.example`
- Modify: `README.md`
- Modify: `docs/LOCAL_WINDOWS_CICD.md`
- Modify: `docs/TECH_DEBT.md`
- Modify: `.env.example`
- Modify: `scripts/ci-workflows.test.mjs`

**Interfaces:**
- Consumes: final commands, ports, environment variables, service names, Swagger URLs, and the deferred work from the design spec.
- Produces: standalone runbooks for each project, a root deployment guide, and explicit future epics that cannot be mistaken for completed work.

- [ ] **Step 1: Add failing documentation contract assertions**

Append to `scripts/ci-workflows.test.mjs`:

```js
test("documentation describes standalone projects and API docs", () => {
  const rootReadme = readFileSync("README.md", "utf8");
  const webReadme = readFileSync("web/README.md", "utf8");
  const backendReadme = readFileSync("backend/README.md", "utf8");
  const windows = readFileSync("docs/LOCAL_WINDOWS_CICD.md", "utf8");
  assert.match(rootReadme, /two independent applications/i);
  assert.match(webReadme, /PUBLIC_API_BASE_URL/);
  assert.match(backendReadme, /CORS_ALLOWED_ORIGINS/);
  assert.match(backendReadme, /http:\/\/localhost:3219\/docs/);
  assert.match(windows, /API_HTTPS_PORT/);
  assert.match(windows, /docker compose logs -f web backend/);
});
```

- [ ] **Step 2: Verify RED**

Run: `node --test scripts/ci-workflows.test.mjs`

Expected: FAIL because standalone README and environment examples do not exist.

- [ ] **Step 3: Write exact standalone and deployment runbooks**

Document these commands verbatim:

```bash
# web only
npm ci --prefix web
PUBLIC_API_BASE_URL=http://localhost:3219 npm run dev --prefix web

# backend only
docker compose up -d postgres
cd backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking \
CORS_ALLOWED_ORIGINS=http://localhost:3000 \
APP_ADDR=:3219 go run ./cmd/api

# complete local stack
docker compose up --build -d web backend postgres

# Swagger
open http://localhost:3219/docs
```

List which environment variables belong to web, backend, and root Compose. Document HTTP/HTTPS URL pairs, certificate installation, firewall ports `3443` and `3444`, independent logs, direct API smoke, persistent uploads, and rollback through the previous Git revision plus `docker compose up --build`.

- [ ] **Step 4: Record deferred technical epics**

Add separate `docs/TECH_DEBT.md` sections with outcomes and acceptance boundaries for:

```text
Access/refresh token authentication with rotation and replay detection
Native mobile authentication and secure token storage
/api/v1 versioning and deprecation policy
Rate limiting, security headers, metrics, tracing, and alerting
Feed product decision: redesign and restore or remove with a data migration
Feature-oriented split of the large Go HTTP package and Redux slice
Separate-resource production deployment definitions
```

State explicitly that current cookie sessions remain supported and that none of these epics are implemented by this refactor.

- [ ] **Step 5: Run full automated verification**

Run:

```bash
npm ci --prefix web
npm run quality
PUBLIC_API_BASE_URL=http://localhost:3219 npm run build --prefix web
cd backend && go test ./... && node --test scripts/api-docs.test.mjs
cd .. && docker compose config
docker compose build web backend
docker compose up -d web backend postgres
node scripts/smoke-stack.mjs
docker compose down
git diff --check
```

Expected: every command PASS with no warnings caused by the refactor. If an unrelated pre-existing test fails, record its exact name and output before deciding whether it blocks deployment.

- [ ] **Step 6: Perform manual route and persistence acceptance**

Using the HTTPS URLs generated by `scripts/setup-lan-https-proxy.ps1`, verify:

```text
/ redirects to /speak
/history refreshes without returning to Speak
/history/<owned-id> reloads the same recording
/profile, /profile/subscription, /profile/english-level, /profile/interests reload
unauthenticated protected routes return through /auth?returnTo=...
unsafe external returnTo values land on /speak
record/save replaces local-* with the permanent recording URL
audio, shadowing audio, photos, delete, logout, and session restore work
no Feed tab, publication button, comments, or /feed web page exists
API /docs lists retained Feed endpoints and Try it out targets API HTTPS 3444
another LAN device can load web HTTPS 3443 and call API HTTPS 3444
```

- [ ] **Step 7: Commit documentation and final readiness changes**

```bash
git add README.md web/README.md web/.env.example backend/README.md backend/.env.example docs/LOCAL_WINDOWS_CICD.md docs/TECH_DEBT.md .env.example scripts/ci-workflows.test.mjs
git commit -m "docs: describe independent web and API deployment"
```

- [ ] **Step 8: Run the final repository status check**

Run:

```bash
git status --short
git log --oneline -12
```

Expected: no unintended uncommitted files; the task commits appear in dependency order and the pre-existing design commit remains in history.
