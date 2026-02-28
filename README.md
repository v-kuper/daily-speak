# Daily Speaking Practice (Next.js + Redux)

A demo speaking-practice app rewritten from static HTML into a modern React stack:

- Next.js (App Router)
- TypeScript
- Redux Toolkit + React Redux

## Run locally

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

## PostgreSQL setup (real auth)

The app now uses PostgreSQL for real authentication:
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

Database schema is created automatically on first request to auth API.

Quick local start with Docker:

```bash
docker compose up -d postgres
cp .env.example .env.local
```

After creating/updating `.env.local`, restart `npm run dev`.

## Ollama setup (daily questions)

The app now uses local Ollama to:
- generate 3 speaking questions for each day
- generate follow-up questions and useful words for a selected topic
- generate mock transcript and grammar suggestions when saving a recording
- personalize generation using selected interests from the Profile screen

Profile flow:
- click the email in top bar to open Profile
- click `My interests` to open a separate interest-selection screen

1. Install and run Ollama locally.
2. Pull a model (default is `gemma3:12b`):

```bash
ollama pull gemma3:12b
```

3. (Optional) configure model/base URL via environment variables:

```bash
export OLLAMA_BASE_URL=http://127.0.0.1:11434
export OLLAMA_MODEL=gemma3:12b
```

## Available scripts

- `npm run dev` - start dev server
- `npm run lint` - run Next.js lint rules
- `npm run build` - production build
- `npm run start` - run production server
