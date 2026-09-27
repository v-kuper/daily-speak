# Daily Speaking Backend

`backend/` is a standalone Go HTTP API. It owns authentication, PostgreSQL
migrations, recordings and uploads, AI/transcription integrations, Feed data,
and the OpenAPI contract. It does not contain or proxy the Next.js application.
Browser, mobile, and other HTTP clients can call the same API contract.

Backend layering, code style, and the workflow for adding features are defined
in [`../docs/BACKEND_DEVELOPMENT.md`](../docs/BACKEND_DEVELOPMENT.md). Coding
agents also receive the same rules through the repository `AGENTS.md`.

Paid recording work is executed by the independent `cmd/worker` process. API
requests atomically persist recording state and a PostgreSQL job; workers use
leased claims, heartbeats, bounded concurrency, and retry backoff. API replicas
stay stateless and never start transcription, analysis, TTS, or media-cleanup
goroutines.

## Public surface

- `/api/v1/*`: stable, versioned mobile API endpoints;
- `/api/*`: legacy web application endpoints retained during migration;
- `/uploads/*`: backend-owned persisted media;
- `/healthz`: process liveness;
- `/readyz`: PostgreSQL and durable-queue readiness;
- `/metrics`: protected Prometheus metrics when an operations token is configured;
- `/openapi.json`: canonical OpenAPI 3.1 document;
- `/docs`: Swagger UI.

Unmatched web-style paths such as `/`, `/speak`, or `/history/...` return a JSON
`404`; they belong to the web application.

## Run independently

From the repository root, start only PostgreSQL:

```bash
docker compose up -d postgres
```

Then run the API from its own project directory:

```bash
cd backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking \
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3219 \
APP_ADDR=:3219 go run ./cmd/api
```

In a second process, start the worker with the same `DATABASE_URL`, provider
configuration, and `UPLOADS_DIR`:

```bash
cd backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking \
go run ./cmd/worker
```

The API applies pending, versioned SQL migrations at startup. Open the independent API
documentation at [http://localhost:3219/docs](http://localhost:3219/docs), or:

```bash
open http://localhost:3219/docs
```

The OpenAPI document includes the retained Feed endpoints even though the
current web client does not expose Feed UI.

## Migrate legacy local media to S3

Legacy `/uploads/...` assets are moved only by the explicit
`cmd/media-migrate` command. It is never started by the API, worker, Docker
Compose, migrations, or CI deployment. The command always reads from
`MEDIA_LOCAL_DIR` (falling back to the existing `UPLOADS_DIR`) and never deletes
or modifies source media files.

Configure the S3-compatible target and run the default dry-run first:

```bash
cd backend
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking \
MEDIA_STORAGE_DRIVER=s3 \
MEDIA_LOCAL_DIR=/path/to/current/uploads \
MEDIA_S3_REGION=us-east-1 \
MEDIA_S3_BUCKET=daily-speaking-private \
go run ./cmd/media-migrate
```

Dry-run hashes and audits at most 100 legacy assets by default. It does not
write S3 objects or update PostgreSQL. After reviewing its summary, copy and
publish one bounded batch with:

```bash
go run ./cmd/media-migrate --apply --limit=100
```

Use `--asset-id=<media-asset-id>` for a targeted retry. S3 credentials and an
optional `MEDIA_S3_ENDPOINT`/`MEDIA_S3_FORCE_PATH_STYLE` are supplied through
the same server-only variables as the API and worker. Each apply run:

1. reads and hashes the local source;
2. writes to a deterministic private S3 key and verifies size and SHA-256;
3. atomically switches `media_assets.storage_driver`, bucket, key, and verified
   metadata only after verification succeeds.

The operation is resumable: an S3 object left by an interrupted run is reused
only when its size and checksum match. Concurrent runs use a conditional
database update. Failures leave the database pointed at the local source, and
the source file is always retained. Repeat bounded apply runs until a dry-run
reports both `planned=0` and `failed=0`. Back up PostgreSQL and the uploads directory before a
production migration.

The v1 surface includes unified web/mobile identity under `/api/v1/auth/*`, paginated
`GET /api/v1/recordings`, and `GET /api/v1/recordings/{recordingId}`. Every
response includes `X-Request-ID`; v1 errors include a stable machine-readable
code. See `../docs/api-compatibility.md` for versioning and deprecation rules.

The guest onboarding path uses the same Bearer identity and private media
contract. A guest may upload one audio asset (10 MiB maximum) and call
`POST /api/v1/guest/previews` once. The backend verifies that the audio is no
longer than 60 seconds, then returns only a transcript and up to two
high-confidence corrections. Registration or login atomically promotes the
same preview id to a normal recording when account quota allows, reuses a
completed transcript, and queues the full analysis. Each guest session can be
promoted only once, while an existing account may receive a later guest
session under its normal quota. Multipass analysis, rewrite, and shadowing
never run for an unauthenticated guest.

## Environment

`backend/.env.example` lists backend-owned variables. The Go process does not
load dotenv files automatically; export/source the values in your process
manager or shell.

Required for a useful local API:

- `DATABASE_URL`: PostgreSQL connection string;
- `CORS_ALLOWED_ORIGINS`: comma-separated exact browser origins allowed to make
  credentialed requests. Include the web origin and every API origin used to
  open Swagger, so `/docs` can use `Try it out` for POST/PUT/DELETE operations.

Runtime and storage:

- `APP_ADDR`: listen address, default `:3000`;
- `DATABASE_SSL`: set to `true` when PostgreSQL requires TLS. The
  `DATABASE_URL` must then use `sslmode=verify-full` and a certificate trusted
  by the API host (or a configured `sslrootcert`). Unverified TLS and plaintext
  fallback connections are rejected. The local Docker default remains `false`;
- `MEDIA_STORAGE_DRIVER`: media backend, default `local`. The current Windows
  deployment pins this value to `local`, so API and worker keep using the same
  uploads bind mount without requiring S3 credentials;
- `UPLOADS_DIR`: persistent media directory used by the `local` driver, default
  `public/uploads` outside Docker and `/app/uploads` in the image;
- `MEDIA_S3_REGION`, `MEDIA_S3_BUCKET`, and optional `MEDIA_S3_ENDPOINT`:
  server-only S3-compatible target used only when the driver is explicitly
  switched to `s3`;
- `MEDIA_S3_FORCE_PATH_STYLE`: enable path-style addressing for providers that
  require it, default `false`;
- `MEDIA_S3_ACCESS_KEY_ID`, `MEDIA_S3_SECRET_ACCESS_KEY`, and optional
  `MEDIA_S3_SESSION_TOKEN`: server-only credentials. Keep them out of repository
  files and web configuration. Access key and secret key must be supplied
  together when explicit credentials are used;
- `MEDIA_UPLOAD_URL_TTL`: lifetime of a signed media upload request, default
  `15m`;
- `MEDIA_MULTIPART_PART_SIZE_BYTES`: multipart upload part size, default
  `8388608` (8 MiB);
- `MEDIA_SWEEP_INTERVAL`: worker interval for aborting expired multipart uploads
  and deleting unattached expired media, default `15m`;
- `SERVER_LOG_LEVEL`: log threshold such as `info` or `debug`.

Operations and traffic admission:

- `TRUSTED_PROXY_CIDRS`: comma-separated reverse-proxy CIDRs allowed to supply
  `X-Forwarded-For` and `X-Forwarded-Proto`; empty trusts no proxy;
- `RATE_LIMIT_ENABLED` and the `RATE_LIMIT_AUTH_*`, `RATE_LIMIT_WRITE_*`, and
  `RATE_LIMIT_EXPENSIVE_*` request/window pairs configure distributed limits;
- `READINESS_TIMEOUT`, `READINESS_MAX_QUEUE_DEPTH`, and
  `READINESS_MAX_OLDEST_JOB_AGE` define load-balancer admission thresholds;
- `METRICS_BEARER_TOKEN`: server-only secret enabling `/metrics`. Empty keeps
  the metrics endpoint hidden.

The operational model, initial alerts, worker/API scaling policy, backup and
restore procedure, and remote load/failure checks are documented in
[`../docs/BACKEND_OPERATIONS.md`](../docs/BACKEND_OPERATIONS.md).

Durable worker controls:

- `GUEST_PREVIEW_QUEUE_CAPACITY`: global database-backed admission bound for
  queued/running guest previews, default `100`;
- `WORKER_RECORDING_CONCURRENCY`, `WORKER_GUEST_PREVIEW_CONCURRENCY`,
  `WORKER_SHADOWING_CONCURRENCY`, and `WORKER_CLEANUP_CONCURRENCY`: independent
  bounded pools (defaults `1`, `1`, `2`, and `2`);
- `WORKER_POLL_INTERVAL`: idle queue polling interval, default `1s`;
- `WORKER_LEASE_DURATION` and `WORKER_HEARTBEAT_INTERVAL`: crash-recovery
  lease, defaults `2m` and `30s`; heartbeat must be shorter than the lease;
- `WORKER_RETRY_BASE_DELAY` and `WORKER_RETRY_MAX_DELAY`: exponential retry
  bounds, defaults `5s` and `5m`.
- `WORKER_JOB_RETENTION`: retention for completed durable-job bookkeeping,
  default `720h` (30 days). The maintenance worker prunes terminal rows in
  bounded batches; user recordings and media are not deleted by this setting.

Unified identity:

- `AUTH_ACCESS_TOKEN_SECRET`: server-only signing secret with at least 32
  characters. Generate at least 48 random bytes and keep it in the deployment
  secret store. When it is absent, `/api/v1/auth/*` safely returns
  `identity_unavailable` and no authenticated client can sign in;
- `AUTH_ACCESS_TOKEN_ISSUER` and `AUTH_ACCESS_TOKEN_AUDIENCE`: stable token
  scope, defaulting to `daily-speaking-api` and `daily-speaking-mobile`;
- `AUTH_ACCESS_TOKEN_TTL`: short access lifetime, default `15m`;
- `AUTH_REFRESH_TOKEN_TTL`: device-session lifetime, default `720h`;
- `AUTH_GUEST_TOKEN_TTL`: anonymous identity lifetime, default `24h`.

`ffprobe` is required by the worker to verify guest audio duration. It is
included with `ffmpeg` in the backend image. Outside Docker the worker resolves
it next to `WHISPER_FFMPEG_BIN` or from `PATH`; `FFPROBE_BINARY_PATH` can supply
an explicit executable path.

Access tokens are signed JWTs. Refresh tokens are opaque, single-use, and only
their SHA-256 hashes are stored. Refresh replay revokes the complete device
session. Registration or login with a guest Bearer token merges the guest
principal into the user in one database transaction. Mobile clients should
store refresh tokens in OS secure storage and serialize refresh attempts. Web
keeps its access token only in memory and receives its refresh token only in a
scoped HttpOnly cookie.

Browser refresh cookie:

- `SESSION_COOKIE_SECURE`: boolean; use `true` on HTTPS;
- `SESSION_COOKIE_SAME_SITE`: `lax`, `strict`, or `none`;
- `SESSION_COOKIE_DOMAIN`: optional cookie domain.

`SameSite=None` is rejected unless `Secure=true`. Origins are compared exactly;
wildcards, paths, credentials, queries, and fragments are invalid in
`CORS_ALLOWED_ORIGINS`. The refresh cookie remains HttpOnly. It is used only to
rotate credentials at `/api/v1/auth/refresh`; every protected resource route
requires the same Bearer access token used by native clients.

AI and media variables are grouped in the example file:

- `OLLAMA_*` and `AI_ANALYSIS_CONCURRENCY` configure question/analysis calls;
- `WHISPER_*` configures the Python or `whisper.cpp` transcription backend;
- `CARTESIA_*` configures pronunciation audio synthesis.

`CARTESIA_API_KEY` and the `MEDIA_S3_*` credential values are secrets. Never
commit them or expose them through web configuration. S3 settings are optional
in local mode; an unset S3 secret must not block the current Windows deployment.
Uploaded media and Whisper models/cache must use persistent storage in
production.

## Tests and contract checks

From `backend/`:

```bash
go test ./...
node scripts/build-api-docs.mjs --check
node --test scripts/api-docs.test.mjs
```

To intentionally regenerate the deterministic OpenAPI artifact after a contract
change:

```bash
node scripts/build-api-docs.mjs --write
node scripts/build-api-docs.mjs --check
```

The API documentation tests inventory registered routes, require cookie auth on
protected operations, and retain Feed operations. CI also exercises the API
against PostgreSQL and builds `backend/Dockerfile` independently.

## Docker image

The build context is only `backend/`:

```bash
docker build -t daily-speaking-backend backend
```

The runtime image contains the Go API, Python Whisper, and ffmpeg; it contains no
Node.js or Next.js output. Compose mounts only backend-owned persistent paths:

- `${UPLOADS_HOST_DIR}:/app/uploads`;
- `${WHISPER_TOOLS_HOST_DIR}:/app/tools`.

## Local Whisper

From the repository root:

```bash
npm run setup:whisper
npm run check:whisper
```

These commands create and inspect `backend/.venv`,
`backend/tools/whisper/openai-models`, `backend/tools/whisper/cache`, and
`backend/tools/ffmpeg`. See `tools/whisper/README.md` for both supported
backends and exact relative paths.
