# Daily Speaking Practice

Daily Speaking is a monorepo containing two independent applications:

- `web/`: a Next.js 15 client with its own dependency graph, build, tests, and
  Docker image;
- `backend/`: a Go HTTP API with its own module, tests, migrations, OpenAPI
  contract, and Docker image.

The browser calls the configured API origin directly. The web application does
not proxy API traffic, and the backend does not serve or proxy Next.js. A future
mobile client can therefore consume the same API without depending on the web
project.

## Repository layout

```text
web/                     Next.js application
backend/                 Go API, migrations, OpenAPI, Whisper tooling
scripts/                 repository and deployment utilities
docs/                    operations and architecture documentation
docker-compose.yml       local single-host orchestration
```

The root `package.json` only orchestrates project commands. Runtime dependencies
belong to `web/package.json` or `backend/go.mod`.

## Run one project

Web only:

```bash
npm ci --prefix web
PUBLIC_API_BASE_URL=http://localhost:3219 npm run dev --prefix web
```

Backend only, with PostgreSQL supplied by Compose:

```bash
docker compose up -d postgres
cd backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking \
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3219 \
APP_ADDR=:3219 go run ./cmd/api
```

The backend command above must be run from `backend/` so its relative tool paths
resolve correctly. Use the project environment examples as references before
adding optional AI, transcription, or TTS configuration:

```bash
cp web/.env.example web/.env.local
cp backend/.env.example backend/.env
```

Next.js reads `web/.env.local`. The Go binary does not load dotenv files on its
own, so export/source backend values through the shell or process manager.

## Run the complete stack

```bash
cp .env.example .env
docker compose up --build -d --remove-orphans web backend postgres
```

Default HTTP endpoints:

- web: [http://localhost:3218](http://localhost:3218)
- web health: [http://localhost:3218/web-healthz](http://localhost:3218/web-healthz)
- API: [http://localhost:3219](http://localhost:3219)
- API health: [http://localhost:3219/healthz](http://localhost:3219/healthz)
- OpenAPI: [http://localhost:3219/openapi.json](http://localhost:3219/openapi.json)
- Swagger UI: [http://localhost:3219/docs](http://localhost:3219/docs)

Open Swagger directly on macOS with:

```bash
open http://localhost:3219/docs
```

Compose starts separate `web`, `backend`, and `postgres` services. The web
service has no dependency on backend startup and can serve pages while the API
is unavailable. Uploaded audio belongs only to the backend and is stored outside
the image through `UPLOADS_HOST_DIR`.

## Environment ownership

- Web: `PUBLIC_API_BASE_URL` only. It is a public absolute HTTP(S) origin and is
  read at runtime. Do not put server secrets in `web/.env.local`.
- Backend: database, CORS, session-cookie, uploads, Ollama, Whisper, Cartesia,
  logging, and listen-address variables. See `backend/.env.example`.
- Root Compose: host ports, persistent host paths, and values passed to either
  container. See `.env.example`.

For credentialed browser requests, every web origin must appear exactly in
`CORS_ALLOWED_ORIGINS`. Add each API origin that serves Swagger too, because
Swagger `Try it out` sends mutations from that API origin. The current
authentication remains a PostgreSQL-backed, HttpOnly session cookie. On HTTPS set
`SESSION_COOKIE_SECURE=true`. Use
`SESSION_COOKIE_SAME_SITE=none` only for genuinely cross-site web/API origins;
it requires a secure cookie. Access and refresh tokens are a deferred epic, not
part of this refactor.

## Routes and API contract

Next.js App Router owns navigation for `/speak`, `/auth`, `/history`,
`/history/[recordingId]`, `/profile`, `/profile/subscription`,
`/profile/english-level`, and `/profile/interests`. Direct visits and refreshes
preserve the URL-addressable screen and server-backed state.

The unfinished Feed UI and publication controls were removed from the web
client. Feed handlers, persisted data, and OpenAPI operations remain in the
backend so the product decision can be revisited without data loss.

The canonical API contract is `backend/docs/openapi.json`. The backend serves
it at `/openapi.json` and serves Swagger UI at `/docs`, independently of the web
service.

## Quality gates

Commands that do not start the application stack:

```bash
npm ci --prefix web
npm run quality
PUBLIC_API_BASE_URL=http://localhost:3219 npm run build --prefix web
```

Useful project-specific commands:

```bash
npm run quality --prefix web
cd backend && go test ./...
cd backend && node scripts/build-api-docs.mjs --check
cd backend && node --test scripts/api-docs.test.mjs
```

Runtime smoke against an already-running stack is explicit:

```bash
WEB_BASE_URL=http://localhost:3218 \
API_BASE_URL=http://localhost:3219 \
node scripts/smoke-stack.mjs
```

The smoke creates a unique temporary user and recording, verifies direct
credentialed CORS and upload serving, and cleans up the recording/session.

## LAN and Windows deployment

`npm run docker:lan` starts the HTTP services and prints one canonical LAN
hostname pair for web and API; do not mix that IP with `localhost` or
`127.0.0.1` while using cookie authentication. Browser microphone recording
from another device requires HTTPS. The
Windows self-hosted deployment generates two Caddy sites:

| Service | HTTP | HTTPS |
| --- | --- | --- |
| Web | `http://<windows-ipv4>:3218` | `https://<windows-ipv4>:3443` |
| API | `http://<windows-ipv4>:3219` | `https://<windows-ipv4>:3444` |
| Swagger | `http://<windows-ipv4>:3219/docs` | `https://<windows-ipv4>:3444/docs` |

Certificate trust, firewall rules, persistent uploads, CI/CD variables,
diagnostics, and rollback are documented in
[`docs/LOCAL_WINDOWS_CICD.md`](docs/LOCAL_WINDOWS_CICD.md).

## Operations

```bash
docker compose logs -f web backend
docker compose logs -f lan-https
docker compose --project-name daily-speaking down --remove-orphans
```

Stopping Compose without `-v` does not remove the named PostgreSQL volume or the
configured uploads host directory. The explicit project name and
`--remove-orphans` also make transitions to/from revisions with the legacy
single `app` service safe. See the Windows runbook before rollback; the
transition has downtime and requires a current database/uploads backup.

Backend-specific Ollama, Whisper, and Cartesia instructions live in
[`backend/README.md`](backend/README.md). Follow-up architecture work is tracked
in [`docs/TECH_DEBT.md`](docs/TECH_DEBT.md).
