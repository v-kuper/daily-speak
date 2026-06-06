# Daily Speaking Practice (Next.js client + Go API)

A demo speaking-practice app rewritten from static HTML into a modern React stack:

- Next.js (App Router) for the client
- TypeScript
- Redux Toolkit + React Redux
- Go API gateway/backend

## Run locally

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

The production backend now lives in `backend/` and serves the existing `/api/*`
contract. For local API work, run PostgreSQL and start the Go API:

```bash
docker compose up -d postgres
DATABASE_URL=postgres://postgres:postgres@localhost:5432/daily_speaking npm run dev:api
```

`npm run dev` starts the Next.js client only. In Docker, the Go gateway exposes
one public port and proxies non-API requests to Next.js.

## Docker app

Run the full app container plus PostgreSQL:

```bash
npm run docker:app
```

If local port `3000` is already in use:

```bash
APP_PORT=3218 npm run docker:app
```

Then open the selected host port, for example
[http://localhost:3218](http://localhost:3218).

## PostgreSQL setup (real auth)

The Go API uses PostgreSQL for real authentication:
- user registration (`email + password`)
- sign-in
- server-side session storage with HTTP-only cookie
- persisted user interests
- persisted recordings history

Set these variables before running the app:

```bash
export DATABASE_URL=postgres://user:password@localhost:5432/daily_speaking
# Optional: use for managed PostgreSQL with SSL requirement
export DATABASE_SSL=require
```

Database schema is migrated automatically by the Go API at startup from
`backend/migrations/0001_init.sql`.

Quick local start with Docker:

```bash
docker compose up -d postgres
cp .env.example .env.local
```

After creating/updating `.env.local`, restart the relevant process.

## Ollama setup (daily questions)

The Go API uses local/external Ollama to:
- generate 3 speaking questions for each day
- generate follow-up questions and useful words for a selected topic
- generate grammar suggestions for transcripts
- personalize generation using selected interests from the Profile screen

Profile flow:
- click the email in top bar to open Profile
- click `My interests` to open a separate interest-selection screen
- AI model selection is server-side only and is not shown in Profile

1. Install and run Ollama locally.
2. Pull or configure access to the default model (`gemma4:31b-cloud`):

```bash
ollama pull gemma4:31b-cloud
```

3. (Optional) configure model/base URL via environment variables:

```bash
export OLLAMA_BASE_URL=http://127.0.0.1:11434
export OLLAMA_MODEL=gemma4:31b-cloud
export OLLAMA_THINKING_MODEL=true
```

## Local Whisper setup (recording transcription)

Saved recordings support two local backends:
- `openai` (Python `openai/whisper`)
- `cpp` (`whisper.cpp`)

Use `openai/whisper`:

```bash
npm run setup:whisper
```

This creates everything inside the project:
- `.venv` (python + openai-whisper)
- `tools/whisper/openai-models` (downloaded models)
- `tools/whisper/cache` (runtime cache)
- `tools/ffmpeg/bin/ffmpeg` (local ffmpeg symlink)

Project-local env example:

```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=.venv/bin/python
WHISPER_OPENAI_MODEL=base.en
WHISPER_OPENAI_MODEL_DIR=tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=tools/whisper/cache
WHISPER_FFMPEG_BIN=tools/ffmpeg/bin/ffmpeg
WHISPER_LANGUAGE=en
```

`ffmpeg` is required for webm/m4a decoding. `npm run setup:whisper` installs a project-local copy via `imageio-ffmpeg`.
Check setup:

```bash
npm run check:whisper
```

Use `whisper.cpp`:

```bash
export WHISPER_BACKEND=cpp
export WHISPER_BINARY_PATH=/absolute/path/to/whisper-cli
export WHISPER_MODEL_PATH=/absolute/path/to/ggml-base.en.bin
export WHISPER_LANGUAGE=en
export WHISPER_THREADS=4
```

If `WHISPER_BACKEND` is not set, app tries `cpp` first, then falls back to `openai`.
Detailed setup notes: `tools/whisper/README.md`.

To remove everything Whisper-related from this project:

```bash
rm -rf .venv tools/whisper/openai-models tools/whisper/cache tools/whisper/pip-cache tools/ffmpeg/bin
```

In Docker, Ollama and Whisper models/binaries are external dependencies. Mount
or configure them via the existing `WHISPER_*` and `OLLAMA_*` environment
variables.

## Available scripts

- `npm run dev` - start dev server
- `npm run dev:api` - start the Go API gateway
- `npm run backend:test` - run Go backend tests
- `npm run docker:app` - build and start the full Docker app plus PostgreSQL
- `npm run docker:build` - build the Docker app image only
- `npm run docker:logs` - follow app container logs
- `npm run docker:stop` - stop Docker Compose services
- `npm run typecheck` - run TypeScript type checks
- `npm run lint` - run ESLint
- `npm run test:smoke` - run API smoke checks against `SMOKE_BASE_URL`, or start Go API on `SMOKE_PORT`
- `npm run quality` - run typecheck + lint + Go backend tests
- `npm run build` - production build
- `npm run start` - run production server

## Server logs

API routes now use a structured logger with:
- timestamp
- log level
- route scope
- request id
- compact JSON metadata (status, duration, model, attempt, etc.)

Log level can be configured with:

```bash
export SERVER_LOG_LEVEL=debug # debug | info | warn | error
```

Defaults:
- `development` -> `debug`
- `production` -> `info`

## Project docs

- Technical debt audit and refactor roadmap: `docs/TECH_DEBT.md`
