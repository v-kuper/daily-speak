# Web and Backend Separation Design

## Goal

Split Daily Speaking into two independently installable, testable, buildable, and deployable applications: a Next.js web client and a Go HTTP API. The current Windows self-hosted CI/CD deployment must continue to deploy a working system automatically after a push to `main`, while the resulting boundaries also allow the API and web client to move to different production resources later and allow a mobile client to consume the same API.

The refactor also replaces Redux-driven screen switching with real Next.js routes, removes the unfinished Feed experience from the web client without deleting its backend API or data, and makes the backend's OpenAPI contract available through Swagger UI.

## Success criteria

- `web/` builds and runs without the backend source tree or Go toolchain.
- `backend/` builds and runs without the web source tree, Node.js, or a Next.js upstream.
- Browser requests go directly to a configured public API origin; neither Next.js nor the Go API proxies the other application.
- The existing single-machine deployment runs separate web and backend images and preserves PostgreSQL data and uploaded recordings.
- A push to `main` still runs quality gates and deploys the complete application on the existing Windows self-hosted runner without manual process startup.
- The current public web ports remain `3218` for HTTP and `3443` for HTTPS. The API has independent defaults of `3219` for HTTP and `3444` for HTTPS.
- Refreshing or opening a supported web URL directly restores the same route and reloads its server-backed state.
- Feed is absent from the web client, including publication and comments in recording details, while all Feed backend routes and persisted data remain intact.
- The backend serves an accurate OpenAPI document at `/openapi.json` and Swagger UI at `/docs`.

## Non-goals

- Do not replace the current cookie session with access and refresh tokens in this refactor.
- Do not implement OAuth, PKCE, native mobile authentication, API versioning, rate limiting, or a new authorization model.
- Do not delete Feed handlers, database tables, migrations, or OpenAPI operations from the backend.
- Do not redesign the visual language or recording experience beyond changes required for routing and removal of Feed UI.
- Do not preserve an in-progress browser media stream or unsaved local audio blob across a full page refresh.
- Do not deploy the two applications to separate production hosts in this change; make that a configuration-only future deployment.

## Repository boundaries

The repository remains a monorepo for atomic changes and local orchestration, but contains two standalone application projects:

```text
daily-speaking/
├── web/
│   ├── app/
│   ├── src/
│   ├── scripts/
│   ├── package.json
│   ├── package-lock.json
│   ├── next.config.ts
│   ├── tsconfig.json
│   ├── eslint.config.mjs
│   └── Dockerfile
├── backend/
│   ├── cmd/
│   ├── internal/
│   ├── migrations/
│   ├── docs/
│   ├── scripts/
│   ├── tools/
│   ├── go.mod
│   ├── go.sum
│   └── Dockerfile
├── scripts/
├── docs/
├── docker-compose.yml
└── package.json
```

`web/package.json` owns all Next.js, React, Redux, lint, TypeScript, and client test dependencies. `backend/go.mod` owns all API dependencies. Backend-only Whisper setup, checks, and tool documentation move under `backend/`. API documentation and its validation utilities also belong to `backend/`.

The root contains only repository-wide orchestration, deployment documentation, Compose configuration, and compatibility command aliases. A root `package.json` may retain commands such as `npm run quality` and `npm run docker:lan`, but it must not contain product runtime dependencies or make either application require the other to build.

The legacy static `Daily Speaking Practice.html` and design QA material belong to the web project or documentation; they must not be inputs to the backend build.

## Runtime architecture

The application traffic is intentionally direct:

```text
Browser ───────────────► Web public origin ─────► Next.js pages and assets
   └───────────────────► API public origin ─────► Go API, uploads, health, docs

Future mobile client ──► API public origin ─────► the same Go API contract
```

The Go server exposes only:

- `/api/*` application endpoints;
- `/uploads/*` persisted media;
- `/healthz` API health;
- `/openapi.json` OpenAPI;
- `/docs` Swagger UI.

The Go server no longer accepts `NEXT_UPSTREAM_URL`, constructs a reverse proxy, or handles `/` by forwarding to Next.js. An unmatched non-API path returns a normal backend `404`.

The Next.js server exposes only web pages, framework assets, and a web-specific health endpoint. It does not implement API route handlers or proxy `/api`, `/uploads`, or `/healthz` to Go.

The web image obtains `PUBLIC_API_BASE_URL` at container runtime, not at image build time. The server validates it as an absolute `http` or `https` URL, removes a trailing slash, and supplies it to the client-side API layer through initial application configuration. This keeps one immutable web image usable for local, test, and future production API origins. Local development defaults to `http://localhost:3219`; production deployment must supply the value explicitly.

Knowing the public API URL and HTTP contract is the web client's only backend dependency. The backend contains no web URL in source. Its deployment receives permitted browser origins as security configuration rather than application coupling.

## Cross-origin HTTP and current authentication

The existing PostgreSQL-backed HTTP-only session cookie remains the authentication mechanism for this refactor. Its cookie name and existing session records remain valid so the separation does not intentionally sign users out.

All web API requests go through one client module that:

- resolves API paths against `PUBLIC_API_BASE_URL`;
- sends `credentials: "include"`;
- preserves `FormData`, audio chunks, response bodies, abort signals, and request-specific caching options;
- normalizes JSON and network failures into user-safe errors;
- never silently redirects a request to another origin.

The backend adds a CORS middleware with an exact, comma-separated `CORS_ALLOWED_ORIGINS` allowlist. It contains every browser web origin and every API origin used to serve Swagger, so Swagger `Try it out` can perform documented mutations. It supports credentials and only the methods and headers used by the documented API. Preflight responses use the request origin only when it is explicitly allowed; wildcard origins are forbidden with credentials. Browser requests from disallowed origins receive no CORS permission, and state-changing requests with a present but disallowed `Origin` are rejected rather than merely having response headers omitted.

Cookie attributes become explicit backend configuration instead of depending on the Node-specific `NODE_ENV` variable:

- `SESSION_COOKIE_SECURE`;
- `SESSION_COOKIE_SAME_SITE` with validated values `strict`, `lax`, or `none`;
- optional `SESSION_COOKIE_DOMAIN`.

Production HTTPS uses `Secure`. `SameSite=None` is accepted only with `Secure=true`. Local `go run` defaults remain usable over loopback HTTP. Future production should prefer related origins such as `app.example.com` and `api.example.com` until the separate token-auth epic is implemented.

API responses continue to represent stored media with server-owned `/uploads/...` paths. The web API layer resolves those paths against the API origin before passing them to `<audio>` or `<img>`. Data URLs used for unsaved local media remain unchanged. Arbitrary third-party URLs are not reinterpreted as API uploads.

## Web routes and navigation

Next.js App Router becomes the source of truth for page navigation:

| Route | Access | Purpose |
| --- | --- | --- |
| `/` | Public | Redirect to `/speak` |
| `/speak` | Public | Speaking and recording flow |
| `/auth?returnTo=...` | Public | Sign in and registration |
| `/history` | Authenticated | Recording history |
| `/history/[recordingId]` | Authenticated | Recording details |
| `/profile` | Authenticated | Profile home |
| `/profile/subscription` | Authenticated | Subscription settings |
| `/profile/english-level` | Authenticated | English level settings |
| `/profile/interests` | Authenticated | Interest selection |

The root layout retains the Redux provider. A shared web shell performs session restoration and user-data loading, renders the header, and renders route content as `children`. Main navigation and the brand use `Link`; active styles derive from `usePathname` rather than Redux.

`currentScreen`, `activeTab`, and navigation-only reducers are removed. Redux continues to own session state, server-backed application data, recording workflow state, playback state, and temporary form state.

Authenticated route guards wait for session restoration before deciding whether to render. An unauthenticated direct visit redirects to `/auth` with an internal `returnTo`. Only known relative application paths are accepted, preventing external or protocol-relative redirects. Successful login and registration return to that route; cancellation returns to `/speak` when no safe prior route exists. Logout clears client state and replaces the route with `/speak`.

The history date filter is represented by `?date=YYYY-MM-DD`, validated before use, so a refresh preserves it. Calendar visibility and other presentation-only toggles remain ephemeral.

The recording details page receives `recordingId` from the URL. After session restoration it uses already-loaded user data when possible and otherwise requests `GET /api/recordings/{recordingId}`. Missing, unauthorized, or deleted records render a stable not-found/error state with a link to history. A newly saved recording navigates to its permanent route when the server ID is available. If an optimistic local recording is shown before persistence completes, the route is replaced with the permanent ID after success and returns to history with a visible error after terminal failure.

A page refresh restores URL-addressable screens and server-backed data. It does not attempt to resume a live `MediaRecorder`, an in-memory draft password, transient playback position, or an unsent audio blob.

## Removing Feed from the web client

The web project removes:

- the Feed navigation tab;
- `FeedScreen`, `FeedThreadScreen`, and `FeedReactionBar`;
- `ShareModal` and the `Publish to Feed` action;
- Feed publication status, comments, and replies from recording details;
- Feed-specific thunks, reducers, selectors, state, and client-only data types;
- the unused legacy `ShareScreen` and its unreachable navigation actions.

Recording deletion copy must no longer describe Feed behavior to the web user. Backend deletion behavior remains unchanged so deleting a recording still cleans up an associated Feed post when one exists.

No backend Feed handler, database relation, migration, contract test, or OpenAPI operation is deleted. This keeps the unfinished feature available for future web or mobile work without exposing it in the current UI.

## Docker and local deployment

There are two application images:

- `web/Dockerfile` builds the Next.js standalone server from `web/` using Node 22 and contains no Go binary, Whisper runtime, backend source, or server secrets.
- `backend/Dockerfile` builds the Go API from `backend/` and installs only its required runtime packages, including Python Whisper and ffmpeg. It contains no Node.js or Next.js output.

`docker-compose.yml` defines `web`, `backend`, `postgres`, and the existing infrastructure `lan-https` service. `web` depends only on runtime configuration; it must still start and serve pages when the API is unavailable. `backend` depends on PostgreSQL health. Persistent uploads mount only into `backend`. Whisper model/cache storage also mounts only into `backend`.

Default host exposure is:

- web HTTP: `0.0.0.0:3218`;
- API HTTP: `0.0.0.0:3219`;
- web HTTPS: `0.0.0.0:3443`;
- API HTTPS: `0.0.0.0:3444`;
- PostgreSQL remains bound to loopback only.

The generated Caddy configuration has independent web and API sites. The web HTTPS site proxies only to `web:3000`; the API HTTPS site proxies only to `backend:3000`. Both sites and the certificate use one detected LAN address. The LAN deployment does not advertise `localhost` or `127.0.0.1` aliases, because mixing those hosts with the LAN IP would make the current `SameSite=Lax` cookie unusable. The ordinary standalone local pair remains `http://localhost:3218` and `http://localhost:3219`. Caddy is deployment infrastructure, not an application-level dependency between web and backend.

The Windows setup script detects the LAN address, builds the public API URL, supplies matching web and API/Swagger CORS origins for that same hostname, creates firewall rules for both HTTPS ports, and runs all required Compose services. It retains explicit overrides for ports and upload storage. Existing `UPLOADS_HOST_DIR` data is reused without moving, deleting, or recreating recordings.

## CI/CD and rollout

The quality workflow treats each application independently:

1. Install `web/` dependencies from its lockfile.
2. Run web unit tests, typecheck, lint, and `next build` from `web/`.
3. Run `go test ./...` from `backend/` with the test PostgreSQL service.
4. Validate and regenerate the backend OpenAPI artifact, failing on drift.
5. Run backend smoke tests directly against the API process, not through Next.js.
6. Build both Docker images independently.

The Windows deployment workflow preserves its `main`/`master` triggers, runner labels, stable Compose project name, Cartesia validation, external upload directory, and Whisper verification. It builds and starts `web`, `backend`, `postgres`, and `lan-https`, then checks:

- the web health endpoint and `/speak` on the web HTTP port;
- `/healthz` directly on the API HTTP port;
- API CORS preflight for the configured web origin;
- registration, login, session restoration, and logout with a cookie crossing the two configured origins;
- a representative protected API call;
- persisted upload serving from the API origin;
- `/openapi.json` and `/docs` from the API origin;
- both web and API HTTPS endpoints after Caddy starts;
- Whisper and Cartesia configuration inside the backend container only.

The deployment remains an in-place Compose update under the stable `daily-speaking` project and uses scoped orphan removal so the former combined `app` container cannot retain the web port. PostgreSQL volumes and the external uploads directory are not recreated or deleted. Service names and documentation change from `app` to `web` and `backend`. Rollback across that boundary explicitly stops the stable project with orphan removal but without `-v` before the older deployment starts. Failures print separate logs for those services and Caddy so the broken boundary is obvious.

Because the database schema and session token format do not change in this refactor, no data backfill or forced sign-out is planned. The route and container split must be released together through Compose; a partially deployed old web/new backend combination is not a supported intermediate state.

## OpenAPI and Swagger

The canonical OpenAPI document belongs to `backend/` and covers every registered API route, including the retained Feed operations. For each operation it defines authentication, path/query parameters, request bodies, success responses, error responses, and representative examples. Shared schemas describe users, recordings, processing state, suggestions, subscriptions, Feed posts, replies, and errors.

The backend serves the canonical artifact at `/openapi.json` and a Swagger UI entry point at `/docs`. Both are read-only and do not depend on the web container. The documented server list includes local API HTTP/HTTPS examples and a placeholder production API origin rather than the web origin.

Contract tests maintain a route inventory and fail when a registered route is absent from OpenAPI. Schema parsing and deterministic generation prevent a stale generated artifact from passing CI. Feed remains in this inventory even though its client UI is removed.

## Error handling and observability

The web distinguishes API unavailability, CORS/configuration failure, authentication failure, and application validation errors. A missing or invalid production `PUBLIC_API_BASE_URL` fails clearly rather than falling back to the web origin. Protected pages do not redirect until authentication initialization has completed, avoiding route flicker and redirect loops.

The backend logs request scope, request ID, status, and duration as it does today. CORS rejections identify the rejected origin without logging credentials or cookies. Backend health reports backend readiness only; it does not inspect web health. Web health reports web readiness only; it does not fail because the API is temporarily unavailable.

## Testing strategy

Backend tests cover:

- removal of Next.js proxy behavior and correct non-API `404` responses;
- exact-origin CORS allow/deny behavior, preflight, credentials, and unsafe-method rejection;
- validated cookie configuration and preservation of existing session authorization;
- API, upload, health, OpenAPI, and Swagger route registration;
- the existing auth, recording, analysis, subscription, shadowing, deletion, and Feed behavior.

Web tests cover:

- runtime API URL validation and path resolution;
- direct API requests with credentials and correct handling of JSON, `FormData`, chunks, and abort signals;
- API-origin resolution for `/uploads/*` while leaving local data URLs intact;
- safe `returnTo` validation and authenticated route decisions;
- route derivation, history date query parsing, and recording-detail IDs;
- absence of Feed navigation and publication controls;
- the route transition from optimistic to permanent recording IDs.

Repository and Docker tests cover:

- independent Docker build contexts and the absence of the other application's artifacts in each image;
- Compose service names, ports, volumes, environment ownership, and health checks;
- two-site Caddy generation and firewall instructions;
- CI execution of both independent quality gates and post-deploy smoke checks.

Manual acceptance verifies navigation and refresh on every route, recording and playback through the separate HTTPS origins, authentication persistence, profile editing, recording deletion, Swagger exploration, and access from another LAN device.

## Follow-up technical epics

The refactor records but does not implement these follow-ups:

1. Replace the cookie session with short-lived access tokens and rotating refresh tokens, including replay detection, token-family revocation, secure browser storage strategy, and native mobile authentication.
2. Introduce an explicitly versioned `/api/v1` contract with a compatibility and deprecation policy.
3. Add rate limiting, security headers, structured metrics, tracing, and production alerting.
4. Decide whether to redesign and restore Feed or remove its backend surface and data through a separate migration.
5. Evaluate splitting large backend HTTP handlers and the large Redux application slice along bounded feature ownership.
6. Add production deployment definitions for separate web and API resources once their target platform and domains are chosen.

These epics must not expand the present refactor or delay the agreed separation, routing, client Feed removal, CI/CD preservation, and Swagger deliverables.
