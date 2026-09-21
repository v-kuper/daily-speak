# Daily Speaking Backend

`backend/` is a standalone Go HTTP API. It owns authentication, PostgreSQL
migrations, recordings and uploads, AI/transcription integrations, Feed data,
and the OpenAPI contract. It does not contain or proxy the Next.js application.
Browser, mobile, and other HTTP clients can call the same API contract.

## Public surface

- `/api/*`: application endpoints;
- `/uploads/*`: backend-owned persisted media;
- `/healthz`: API health;
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
CORS_ALLOWED_ORIGINS=http://localhost:3000 \
APP_ADDR=:3219 go run ./cmd/api
```

The API applies `migrations/0001_init.sql` at startup. Open the independent API
documentation at [http://localhost:3219/docs](http://localhost:3219/docs), or:

```bash
open http://localhost:3219/docs
```

The OpenAPI document includes the retained Feed endpoints even though the
current web client does not expose Feed UI.

## Environment

`backend/.env.example` lists backend-owned variables. The Go process does not
load dotenv files automatically; export/source the values in your process
manager or shell.

Required for a useful local API:

- `DATABASE_URL`: PostgreSQL connection string;
- `CORS_ALLOWED_ORIGINS`: comma-separated exact web origins allowed to make
  credentialed browser requests.

Runtime and storage:

- `APP_ADDR`: listen address, default `:3000`;
- `DATABASE_SSL`: set to `require`, `true`, `1`, `yes`, or `on` when PostgreSQL
  requires TLS;
- `UPLOADS_DIR`: persistent media directory, default `public/uploads` outside
  Docker and `/app/uploads` in the image;
- `SERVER_LOG_LEVEL`: log threshold such as `info` or `debug`.

Current session authentication:

- `SESSION_COOKIE_SECURE`: boolean; use `true` on HTTPS;
- `SESSION_COOKIE_SAME_SITE`: `lax`, `strict`, or `none`;
- `SESSION_COOKIE_DOMAIN`: optional cookie domain.

`SameSite=None` is rejected unless `Secure=true`. Origins are compared exactly;
wildcards, paths, credentials, queries, and fragments are invalid in
`CORS_ALLOWED_ORIGINS`. The session cookie remains HttpOnly and its records are
stored in PostgreSQL. Access/refresh tokens are not implemented by this
refactor.

AI and media variables are grouped in the example file:

- `OLLAMA_*` and `AI_ANALYSIS_CONCURRENCY` configure question/analysis calls;
- `WHISPER_*` configures the Python or `whisper.cpp` transcription backend;
- `CARTESIA_*` configures pronunciation audio synthesis.

`CARTESIA_API_KEY` is a secret. Never commit it or expose it through web
configuration. Uploaded media and Whisper models/cache must use persistent
storage in production.

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
