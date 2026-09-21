# Daily Speaking Web

`web/` is a standalone Next.js application. It owns the browser UI, App Router
routes, Redux state, client tests, and its Docker image. It contains no Go code
and does not proxy API or upload requests.

## Configure

Copy the example and point the browser at a reachable API origin:

```bash
cp web/.env.example web/.env.local
```

```dotenv
PUBLIC_API_BASE_URL=http://localhost:3219
```

`PUBLIC_API_BASE_URL` must be an absolute `http` or `https` URL without
credentials, a query, or a fragment. It is public runtime configuration, not a
secret. Production must set it explicitly. The API must list the web origin in
its `CORS_ALLOWED_ORIGINS` and allow credentialed requests.

## Install and run

From the repository root:

```bash
npm ci --prefix web
PUBLIC_API_BASE_URL=http://localhost:3219 npm run dev --prefix web
```

Open [http://localhost:3000/speak](http://localhost:3000/speak). The web app can
start and serve pages while the API is unavailable; API-backed screens then show
their normal availability errors.

## Routes

| Route | Access | Purpose |
| --- | --- | --- |
| `/` | public | redirects to `/speak` |
| `/speak` | public | speaking and recording flow |
| `/auth?returnTo=...` | public | registration and sign-in |
| `/history` | authenticated | recording history |
| `/history/[recordingId]` | authenticated | recording details |
| `/profile` | authenticated | profile home |
| `/profile/subscription` | authenticated | subscription settings |
| `/profile/english-level` | authenticated | level settings |
| `/profile/interests` | authenticated | interest selection |

Authenticated routes restore the current cookie session before redirecting.
Only validated internal `returnTo` values are accepted. The history date filter
is stored in `?date=YYYY-MM-DD`, so direct visits and refreshes retain it.

Feed, publication, and comment UI are intentionally absent. The backend still
retains its Feed API and data for a later product decision.

## Test and build

```bash
npm run quality --prefix web
PUBLIC_API_BASE_URL=http://localhost:3219 npm run build --prefix web
```

`quality` runs TypeScript checks, ESLint, and client tests. The production image
is built only from this directory:

```bash
docker build -t daily-speaking-web web
```

At container runtime, supply `PUBLIC_API_BASE_URL`; the image does not contain
backend source or server credentials. The web-only health endpoint is
`/web-healthz` and returns `service: "web"` without probing the API.

## Authentication boundary

The client currently sends `credentials: "include"` to the configured API
origin. Authentication is a backend-owned, HttpOnly session cookie. Do not add
token storage or server secrets to the web project. Access/refresh-token support
is tracked as future work in `../docs/TECH_DEBT.md`.
