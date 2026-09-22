# Local Windows CI/CD

The Windows deployment runs two independent application containers plus shared
infrastructure on one test host:

- `web`: Next.js pages and `/web-healthz`;
- `backend`: Go API, uploads, `/healthz`, `/openapi.json`, and `/docs`;
- `postgres`: persistent application database;
- `lan-https`: two independent Caddy HTTPS sites.

The web container receives the public API origin and the canonical HTTPS web
origin. The latter redirects direct HTTP browser access before session-cookie
authentication begins. The backend receives an exact browser-origin CORS
allowlist for the web and Swagger/API origins and does not know or proxy the web
application.
The same images can later move to different production resources by supplying
their public origins through deployment configuration.

## Host layout and prerequisites

Use a repository-specific runner outside the developer checkout and keep uploads
outside both locations:

```text
D:\Projects\daily-speak
D:\Runners\daily-speaking
D:\DailySpeaking\data\uploads
```

The deploy job uses its own checkout under the runner work directory. The
external uploads directory survives checkout cleanup and image replacement.
PostgreSQL data stays in the named `postgres_data` Compose volume.

Install and start:

1. Docker Desktop;
2. Git for Windows;
3. a GitHub Actions self-hosted Windows x64 runner;
4. `winget`, or a preinstalled `mkcert` on the runner PATH.

Verify Docker from PowerShell:

```powershell
docker version
docker compose version
```

Node.js and Go are installed by the workflow and do not need to be permanent
host installations.

## Configure the self-hosted runner

Create the runner in GitHub under `Settings` -> `Actions` -> `Runners` and add
the project label `daily-speaking`. The workflow requires all three labels:

```yaml
runs-on: [self-hosted, windows, daily-speaking]
```

Example GitHub-provided setup command shape:

```powershell
.\config.cmd --url https://github.com/v-kuper/daily-speak --token <one-time-token> --name daily-speaking-windows --labels daily-speaking
```

Configure it as a Windows service for unattended deploys. The service account
must be able to use Docker Desktop and write the uploads directory. Inspect the
service with:

```powershell
Get-Service "actions.runner.*"
```

## GitHub configuration

The deployment workflow is `.github/workflows/deploy-local.yml`. It runs on a
push to `main` or `master`, and can also be started with `workflow_dispatch`.
It uses the stable Compose project name `daily-speaking`.

Required GitHub Actions values:

- secret `CARTESIA_API_KEY`;
- variable `CARTESIA_VOICE_ID`.

The API key must remain in `Secrets`, never `Variables`, repository files,
runner system variables, issue text, or logs. The workflow validates only that
both Cartesia values are non-empty and does not print them.

Optional repository variables and defaults:

| Variable | Default | Owner |
| --- | --- | --- |
| `APP_PORT` | `3218` | web HTTP host port |
| `API_PORT` | `3219` | API HTTP host port |
| `HTTPS_PORT` | `3443` | web HTTPS host port |
| `API_HTTPS_PORT` | `3444` | API HTTPS host port |
| `POSTGRES_PORT` | `5433` | loopback-only database port |
| `UPLOADS_HOST_DIR` | `D:\DailySpeaking\data\uploads` | backend media storage |
| `OLLAMA_BASE_URL` | `http://host.docker.internal:11434` | backend AI service |
| `OLLAMA_MODEL` | `gemma4:31b-cloud` | backend AI model |
| `OLLAMA_THINKING_MODEL` | `true` | backend model behavior |
| `WHISPER_BINARY_PATH` | empty | optional `whisper.cpp` binary |
| `WHISPER_MODEL_PATH` | empty | optional `whisper.cpp` model |

The workflow deliberately fixes `AI_ANALYSIS_CONCURRENCY=3`,
`WHISPER_BACKEND=openai`, `WHISPER_OPENAI_MODEL=base`, and
`WHISPER_LANGUAGE=auto` in source so old runner variables cannot silently alter
the deployed transcription mode.

## Deployment flow

The workflow:

1. checks out the same revision for both projects;
2. installs from `web/package-lock.json` and runs the repository quality gates;
3. validates Docker and Cartesia configuration;
4. runs `.\scripts\setup-lan-https-proxy.ps1`;
5. builds and starts `web`, `backend`, `postgres`, and `lan-https` together with
   `--remove-orphans` under the stable `daily-speaking` Compose project;
6. runs `scripts/smoke-stack.mjs` against the separate HTTP origins;
7. verifies the independent HTTPS health/docs endpoints;
8. verifies Whisper and Cartesia inside `backend` only.

The smoke uses a unique temporary account and verifies web health, `/speak`, API
health, OpenAPI, Swagger, exact credentialed CORS, registration/session, a
protected call, upload creation/serving/deletion, and logout. It never prints
the session cookie.

`--remove-orphans` is the one-time-safe migration from the former `app` service
as well as the normal update behavior. Compose removes the old
`daily-speaking-app` container before converging the new services, so it cannot
retain `APP_PORT`. This does not pass `-v`: the named PostgreSQL volume and the
external `UPLOADS_HOST_DIR` remain intact.

Manual deployment after the workflow is present on the selected branch:

1. Open GitHub `Actions`.
2. Select `Deploy Local Windows`.
3. Select `Run workflow`.

## HTTP and HTTPS endpoints

For Windows address `<windows-ipv4>`:

| Purpose | HTTP | HTTPS |
| --- | --- | --- |
| Web | `http://<windows-ipv4>:3218` | `https://<windows-ipv4>:3443` |
| Web health | `http://<windows-ipv4>:3218/web-healthz` | `https://<windows-ipv4>:3443/web-healthz` |
| API health | `http://<windows-ipv4>:3219/healthz` | `https://<windows-ipv4>:3444/healthz` |
| Swagger | `http://<windows-ipv4>:3219/docs` | `https://<windows-ipv4>:3444/docs` |
| OpenAPI | `http://<windows-ipv4>:3219/openapi.json` | `https://<windows-ipv4>:3444/openapi.json` |

Direct browser visits to the HTTP web address redirect to the HTTPS web address;
the HTTP `/web-healthz` endpoint remains available to deployment checks. The
generated web Caddy site proxies only to `web:3000`; the API site proxies
only to `backend:3000`. Both sites and the generated certificate use the same
detected `<windows-ipv4>` hostname; the LAN helper intentionally does not add
`localhost` or `127.0.0.1` aliases. Use the displayed hostname consistently for
both origins. The deployment sets:

- `PUBLIC_WEB_BASE_URL=https://<windows-ipv4>:3443` and
  `PUBLIC_API_BASE_URL=https://<windows-ipv4>:3444` for the web container;
- the matching HTTP and HTTPS web origins and API/Swagger origins in
  `CORS_ALLOWED_ORIGINS`;
- `SESSION_COOKIE_SECURE=true` and `SESSION_COOKIE_SAME_SITE=lax` for the API.

These LAN origins share a site (the same host), so the current HttpOnly session
cookie can cross the two origins through `credentials: include`. If future web
and API domains are genuinely cross-site, review the cookie threat model and use
`SameSite=None` only with HTTPS, or complete the access/refresh-token epic.

## Certificate and firewall setup

Run the same deployment helper manually only for setup or troubleshooting:

```powershell
cd D:\Projects\daily-speak
.\scripts\setup-lan-https-proxy.ps1 -HostIp <windows-ipv4>
```

The script detects the LAN address when `-HostIp` is omitted, installs/uses
`mkcert`, creates `lan-https\Caddyfile` and certificate files, prepares the
uploads directory, sets the runtime origins, and starts all four services.

Allow both HTTPS ports from an elevated PowerShell if the runner cannot create
the rules:

```powershell
New-NetFirewallRule -DisplayName "Daily Speaking HTTPS 3443" -Direction Inbound -Protocol TCP -LocalPort 3443 -Action Allow
New-NetFirewallRule -DisplayName "Daily Speaking HTTPS 3444" -Direction Inbound -Protocol TCP -LocalPort 3444 -Action Allow
```

For optional plain-HTTP LAN diagnostics, also allow `3218` and `3219`. Browser
microphone access on another device requires the HTTPS web URL.

Client devices must trust the runner's local mkcert root CA. Locate it on the
runner with:

```powershell
mkcert -CAROOT
```

Copy only `rootCA.pem` to each client and import it into the trusted root store.
Never copy `rootCA-key.pem`.

## Persistent uploads

The backend container sees uploaded files at `/app/uploads`; the default
Windows host directory is:

```text
D:\DailySpeaking\data\uploads
```

Recorded media is stored below `recordings\<user-id>`, Feed reply media below
`feed-replies\<user-id>`, and generated pronunciation audio below
`shadowing\<user-id>`. PostgreSQL stores `/uploads/...` URLs, and the backend
serves those paths from the API origin. The web container has no upload mount.

Set the `UPLOADS_HOST_DIR` repository variable to move media to another durable
drive. Never point it at the Actions checkout. Do not use `docker compose down
-v` in normal operations because `-v` removes the PostgreSQL volume.

## Direct checks and logs

Against an already-running Windows LAN stack, use the same hostname that the
setup helper printed and placed in `LAN_HOST_IP` for CI:

```powershell
$HostIp = "<windows-ipv4>"
$env:WEB_BASE_URL = "http://${HostIp}:3218"
$env:API_BASE_URL = "http://${HostIp}:3219"
node scripts/smoke-stack.mjs
```

Direct read-only API checks:

```powershell
Invoke-RestMethod "http://${HostIp}:3219/healthz"
Invoke-WebRequest "http://${HostIp}:3219/openapi.json" -UseBasicParsing
Invoke-WebRequest "http://${HostIp}:3219/docs" -UseBasicParsing
```

Inspect each failure boundary separately:

```powershell
docker compose ps
docker compose logs -f web backend
docker compose logs -f lan-https
docker compose logs -f postgres
```

Safe Cartesia presence check (does not print values):

```powershell
docker compose exec -T backend sh -lc 'test -n "$CARTESIA_API_KEY" && test -n "$CARTESIA_VOICE_ID" && echo cartesia-config-ok'
```

Do not run `env | grep CARTESIA` or publish `docker compose config` output when a
real API key is present.

## Rollback

An ordinary deployment is an in-place Compose update, but crossing the service
split boundary needs an explicit transition. Before rollback, take a current
PostgreSQL backup and a filesystem backup of `UPLOADS_HOST_DIR`, and confirm the
older binary is compatible with the current schema. Expect downtime from the
`down` command until the older stack passes its health checks.

Select the previously known-good revision in a clean checkout/worktree and
preserve the same project name, uploads path, ports, and secrets. After checking
out that older revision, stop both its declared services and any newer orphaned
`web`, `backend`, or `lan-https` containers, without deleting volumes:

```powershell
git switch --detach <previous-good-revision>
$env:COMPOSE_PROJECT_NAME = "daily-speaking"
$env:UPLOADS_HOST_DIR = "D:\DailySpeaking\data\uploads"
docker compose --project-name daily-speaking down --remove-orphans
.\scripts\setup-lan-https-proxy.ps1 -HostIp <windows-ipv4> -SkipCertificateGeneration
```

The `down --remove-orphans` step is required before starting a pre-split
revision; otherwise the newer `web` container can keep `APP_PORT` while the old
`app` container starts. Do not add `-v`: the `daily-speaking` PostgreSQL volume
is preserved, and Compose never deletes the external uploads directory.

For HTTP-only recovery, with the required origins already set in the shell:

```powershell
docker compose up --build -d --remove-orphans web backend postgres
```

Do not delete or recreate the uploads directory or PostgreSQL volume during a
rollback. The split itself does not change the schema or session-token format,
so no backfill or intentional sign-out is required; this does not replace the
backup and schema-compatibility check for later revisions.

## Post-deploy acceptance

The following runtime acceptance belongs to the remote Windows deployment and
must not be inferred from static/local tests:

- `/` redirects to `/speak`;
- `/history`, recording details, and every profile route survive refresh;
- protected routes return through a safe `/auth?returnTo=...` flow;
- recording save replaces a temporary `local-*` URL with the permanent ID;
- original/shadowing audio, photos, deletion, logout, and session restore work;
- no Feed tab, publication button, comments, or `/feed` web page is exposed;
- API Swagger still lists the retained Feed endpoints and targets port `3444`;
- a second LAN device loads web HTTPS `3443` and calls API HTTPS `3444`.

For this refactor, those checks are explicitly deferred to the first remote
CI/CD deployment because the local development environment is not configured to
run the stack.

Official GitHub references:

- [Hosting your own runners](https://docs.github.com/en/actions/how-tos/hosting-your-own-runners?platform=windows)
- [Using self-hosted runners in a workflow](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/use-in-a-workflow)
- [Configuring the runner application as a service](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/configure-the-application)
