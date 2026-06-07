# Local Windows CI/CD

This project deploys as one Docker app container plus a PostgreSQL service. You
need one GitHub Actions self-hosted runner for the whole project, not separate
runners for the Next.js client and Go API.

This guide assumes a repo-level runner for this repository. If every repository
has its own runner on the same Windows host, keep runner directories outside the
project checkout. A clean layout is:

```text
D:\Projects\daily-speak
D:\Runners\daily-speaking
```

Do not put the runner application inside `D:\Projects\daily-speak`. The runner
stores service files, logs, credentials, work directories, and auto-update files
that should not be mixed with repository files.

`D:\Projects\daily-speak` is the developer checkout you edit manually. GitHub
Actions jobs use their own checkout under the runner work directory, for
example `D:\Runners\daily-speaking\_work\daily-speak\daily-speak`, after the
workflow runs `actions/checkout`.

The deploy workflow is `.github/workflows/deploy-local.yml`. It targets:

```yaml
runs-on: [self-hosted, windows, daily-speaking]
```

GitHub routes a job only to a runner that has all requested labels. The runner
will already have `self-hosted` and `windows`; add `daily-speaking` as the
project-specific label.

## Windows machine prerequisites

1. Install Docker Desktop.
2. Start Docker Desktop and keep it running.
3. Make sure this works in PowerShell:

```powershell
docker version
docker compose version
```

4. Install Git for Windows if it is not already installed.
5. Allow inbound TCP port `3218` in Windows Defender Firewall if you need to
open the app from another device on the LAN.

Node.js and Go are installed by the workflow with `actions/setup-node` and
`actions/setup-go`, so they do not need to be permanently installed on the
Windows host for GitHub Actions deploys.

## Add the runner in GitHub

1. Open the GitHub repository.
2. Go to `Settings` -> `Actions` -> `Runners`.
3. Click `New self-hosted runner`.
4. Choose `Windows` and `x64`.
5. Copy the commands shown by GitHub into PowerShell on the Windows machine.
6. When configuring the runner, add this custom label:

```text
daily-speaking
```

If you configure via command flags, include:

```powershell
.\config.cmd --url https://github.com/v-kuper/daily-speak --token <token> --name daily-speaking-windows --labels daily-speaking
```

Use the real URL and one-time token from GitHub's runner setup page.

Example local directory setup:

```powershell
New-Item -ItemType Directory -Force D:\Projects\daily-speak
New-Item -ItemType Directory -Force D:\Runners\daily-speaking
```

Clone the project into `D:\Projects\daily-speak` for local development.
Download and configure the GitHub Actions runner inside
`D:\Runners\daily-speaking`.

## Run as a service

For unattended deploys, configure the runner as a Windows service during runner
setup. GitHub's Windows runner setup asks about this during configuration. If
the runner is already configured without service mode, remove it from GitHub
and configure it again.

After setup, check the service:

```powershell
Get-Service "actions.runner.*"
```

The runner service account must be able to use Docker Desktop. If deploy jobs
fail with Docker connection errors, run the runner interactively first to verify
the pipeline, then adjust the service account/Docker Desktop access.

## Deploy

Automatic deploy:
- push to `main` or `master`
- GitHub runs `.github/workflows/deploy-local.yml`
- the Windows runner runs `npm run quality`
- the runner runs `.\scripts\setup-lan-https-proxy.ps1`
- the script creates `lan-https` cert/config files in the checkout
- the script opens inbound TCP `HTTPS_PORT` in Windows Firewall
- the script runs `docker compose up --build -d app postgres lan-https`
- the workflow checks `https://127.0.0.1:3443/healthz`

Manual deploy:
1. Open `Actions` in GitHub.
2. Select `Deploy Local Windows`.
3. Click `Run workflow`.

## Access from LAN

After a successful deploy, open the app from another device on the same network:

```text
http://<windows-ipv4>:3218
```

On the Windows machine, find the IPv4 address with:

```powershell
ipconfig
```

The HTTP URL is enough for checking that the app loads, but browser microphone
recording requires HTTPS. The deploy workflow runs
`.\scripts\setup-lan-https-proxy.ps1` automatically on the Windows runner. The
script creates certificates and a Caddy config inside the checked-out project,
then starts a `lan-https` Caddy container through Docker Compose.

If the checkout is the documented `D:\Projects\daily-speak`, the script creates:

```text
D:\Projects\daily-speak\lan-https
```

From PowerShell in `D:\Projects\daily-speak`, run:

```powershell
.\scripts\setup-lan-https-proxy.ps1 -HostIp <windows-ipv4>
```

Manual execution is only needed for local troubleshooting; CI/CD runs the same
script during deploy.

For example:

```powershell
.\scripts\setup-lan-https-proxy.ps1 -HostIp 192.168.0.115
```

The script writes:

```text
D:\Projects\daily-speak\lan-https\certs\daily-speaking.pem
D:\Projects\daily-speak\lan-https\certs\daily-speaking-key.pem
D:\Projects\daily-speak\lan-https\Caddyfile
```

It also runs:

```powershell
docker compose up --build -d app postgres lan-https
```

The script tries to allow the HTTPS port in Windows Firewall automatically:

```powershell
New-NetFirewallRule -DisplayName "Daily Speaking HTTPS 3443" -Direction Inbound -Protocol TCP -LocalPort 3443 -Action Allow
```

If the runner service account cannot create firewall rules, the script prints a
warning and continues the deploy. LAN clients may still be blocked until you run
the same command once from an elevated PowerShell or run the runner service with
an account that can manage Windows Firewall.

To restart the Docker app with HTTPS later:

```powershell
cd D:\Projects\daily-speak
$env:HTTPS_PORT=3443; docker compose up --build -d app postgres lan-https
```

Then open the HTTPS URL from another LAN device:

```text
https://<windows-ipv4>:3443
```

The Caddy container proxies HTTPS traffic to the Docker Compose `app` service
at `app:3000`. Do not redirect clients to `localhost`.

The script can install `mkcert` on the runner via `winget` when it is missing.
The generated certificate is still a local certificate. Client machines must
trust the runner's mkcert root CA once, otherwise their browser will show a
certificate warning and microphone access may still be blocked. On the Windows
runner, find that CA with:

```powershell
mkcert -CAROOT
```

Copy only `rootCA.pem` to client machines and import it into Trusted Root
Certification Authorities. Do not copy `rootCA-key.pem`.

## Optional variables

The workflow uses GitHub repository variables when present:

- `APP_PORT`, default `3218`
- `HTTPS_PORT`, default `3443`
- `POSTGRES_PORT`, default `5433`
- `OLLAMA_BASE_URL`, default `http://host.docker.internal:11434`
- `OLLAMA_MODEL`, default `gemma4:31b-cloud`
- `OLLAMA_THINKING_MODEL`, default `true`
- `WHISPER_BACKEND`, default `openai`
- `WHISPER_BINARY_PATH`
- `WHISPER_MODEL_PATH`
- `WHISPER_PYTHON_BIN`, default `/opt/whisper/bin/python`
- `WHISPER_OPENAI_MODEL`, default `base.en`
- `WHISPER_OPENAI_MODEL_DIR`, default `/app/tools/whisper/openai-models`
- `WHISPER_OPENAI_CACHE_DIR`, default `/app/tools/whisper/cache`
- `WHISPER_FFMPEG_BIN`, default `/usr/bin/ffmpeg`
- `WHISPER_OPENAI_DEVICE`, default `cpu`
- `WHISPER_OPENAI_FP16`, default `false`
- `WHISPER_LANGUAGE`, default `en`

Set them in `Settings` -> `Secrets and variables` -> `Actions` -> `Variables`.

If several projects deploy on the same Windows machine, give each project a
unique `APP_PORT` and `POSTGRES_PORT` to avoid host-port conflicts. The deploy
workflow also sets `COMPOSE_PROJECT_NAME=daily-speaking` so Docker Compose uses
the same project name regardless of the runner checkout directory.

## Useful commands on the Windows machine

```powershell
npm run docker:lan
docker compose ps
docker compose logs -f app
docker compose down
```

Official GitHub references:
- [Hosting your own runners](https://docs.github.com/en/actions/how-tos/hosting-your-own-runners?platform=windows)
- [Using self-hosted runners in a workflow](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/use-in-a-workflow)
- [Configuring the runner application as a service](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/configure-the-application)
