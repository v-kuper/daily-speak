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

Then open [http://localhost:3218](http://localhost:3218).

Microphone recording works from `localhost` in modern browsers, but remote
addresses must be served over HTTPS. If you open the Docker app from another
machine as `http://<ip>:3218`, the page can load, but the browser will not show
the microphone permission prompt.

If you want a different host port:

```bash
APP_PORT=3000 npm run docker:app
```

## Windows/LAN hosting

On a Windows machine on your local network:

```bash
npm install
npm run docker:lan
```

This single command builds the Next.js client, builds the Go API, starts the
app container, starts PostgreSQL, and prints local-network URLs for the host
machine. From another device on the same LAN, open the printed address, for
example:

```text
http://192.168.1.42:3218
```

Those LAN HTTP URLs are useful for checking layout and backend connectivity, but
browser microphone recording requires `https://...` or `http://localhost`. For a
phone or another computer, put the app behind an HTTPS reverse proxy or tunnel
such as Caddy, nginx with TLS, Cloudflare Tunnel, or another trusted certificate
setup, then open the HTTPS URL.

You do not need to start the frontend, backend, or database separately. The app
container exposes only the app port to the LAN. PostgreSQL stays inside Docker
and is bound to `127.0.0.1` on the host for local admin tools.

If another device cannot connect:
- make sure both devices are on the same Wi-Fi/LAN
- allow inbound TCP port `3218` in Windows Defender Firewall / Docker Desktop
- run `ipconfig` on Windows and use the IPv4 address of that machine

PowerShell port override:

```powershell
$env:APP_PORT=8080; npm run docker:lan
```

CI/CD setup for the Windows LAN host is documented in
`docs/LOCAL_WINDOWS_CICD.md`.

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
export AI_ANALYSIS_CONCURRENCY=3
```

`AI_ANALYSIS_CONCURRENCY` controls how many independent error-detector requests
can run at the same time. Values from 1 to 7 are accepted; missing or invalid
values fall back to 3. Each detector keeps its own focused prompt rather than
combining error types into one request. Raising the value can increase Ollama
CPU, GPU, and memory load.

## Local Whisper setup (recording transcription)

Saved recordings support two local backends:
- `openai` (Python `openai/whisper`)
- `cpp` (`whisper.cpp`)

Docker deploys use the same local Python Whisper backend. The image installs
Linux Python, `openai-whisper`, and `ffmpeg`; `docker-compose.yml` mounts
`./tools` into `/app/tools` so downloaded models and cache survive rebuilds.
The first transcription can download the configured model if it is not already
in `tools/whisper/openai-models`.

```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=/opt/whisper/bin/python
WHISPER_OPENAI_MODEL=base
WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
WHISPER_OPENAI_DEVICE=cpu
WHISPER_OPENAI_FP16=false
WHISPER_LANGUAGE=auto
```

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
WHISPER_OPENAI_MODEL=base
WHISPER_OPENAI_MODEL_DIR=tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=tools/whisper/cache
WHISPER_FFMPEG_BIN=tools/ffmpeg/bin/ffmpeg
WHISPER_LANGUAGE=auto
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
export WHISPER_MODEL_PATH=/absolute/path/to/ggml-base.bin
export WHISPER_LANGUAGE=auto
export WHISPER_THREADS=4
```

If `WHISPER_BACKEND` is not set outside Docker, app tries `cpp` first, then
falls back to local Python `openai/whisper`.
The multilingual model and automatic language detection preserve occasional
Russian words in otherwise English recordings so they can be corrected by the
AI suggestions step.
Detailed setup notes: `tools/whisper/README.md`.

To remove everything Whisper-related from this project:

```bash
rm -rf .venv tools/whisper/openai-models tools/whisper/cache tools/whisper/pip-cache tools/ffmpeg/bin
```

In Docker, Ollama remains external by default. Whisper runs inside the app
container through local Python `openai-whisper`.

## Cartesia shadowing audio

The recording details screen can generate a separate pronunciation track for
the corrected text. New recordings start synthesis automatically after the AI
rewrite is ready; older recordings start synthesis when you open them.

For the Docker setup, create a repository-root `.env` from the committed
template:

```bash
cp .env.example .env
```

Open `.env`, paste the Cartesia API key after `CARTESIA_API_KEY=`, and paste the
UUID of the chosen natural female American voice after `CARTESIA_VOICE_ID=`.
Keep the remaining defaults unless your Cartesia account requires different
values. Then rebuild and follow the app logs:

```bash
docker compose up --build -d app postgres
docker compose logs -f app
```

The repository ignores `.env`. Never post the API key in chat or commit it to
Git. The key is server-only; there is no client-prefixed Cartesia variable.

The Windows GitHub Actions deployment does not need a `.env` file. It reads
`CARTESIA_API_KEY` from GitHub Actions Secrets and `CARTESIA_VOICE_ID` from
GitHub Actions Variables. See `docs/LOCAL_WINDOWS_CICD.md` for the exact setup
and verification checklist.

## Available scripts

- `npm run dev` - start dev server
- `npm run dev:api` - start the Go API gateway
- `npm run backend:test` - run Go backend tests
- `npm run docker:app` - build and start the full Docker app plus PostgreSQL
- `npm run docker:lan` - build/start Docker app plus PostgreSQL and print LAN URLs
- `npm run docker:build` - build the Docker app image only
- `npm run docker:logs` - follow app container logs
- `npm run docker:stop` - stop Docker Compose services
- `npm run typecheck` - run TypeScript type checks
- `npm run lint` - run ESLint
- `npm run test:smoke` - run API smoke checks against `SMOKE_BASE_URL`, or start Go API on `SMOKE_PORT`
- `npm run test:docker-lan` - run LAN helper unit tests
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

- Local Windows CI/CD setup: `docs/LOCAL_WINDOWS_CICD.md`
- Technical debt audit and refactor roadmap: `docs/TECH_DEBT.md`
