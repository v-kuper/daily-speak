# Local Windows CI/CD

This project deploys as one Docker app container plus a PostgreSQL service. You
need one GitHub Actions self-hosted runner for the whole project, not separate
runners for the Next.js client and Go API.

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

4. Allow inbound TCP port `3218` in Windows Defender Firewall if you need to
open the app from another device on the LAN.

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
.\config.cmd --url https://github.com/<owner>/<repo> --token <token> --labels daily-speaking
```

Use the real URL and one-time token from GitHub's runner setup page.

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
- the runner runs `npm run docker:lan`
- the workflow checks `http://127.0.0.1:3218/healthz`

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

## Optional variables

The workflow uses GitHub repository variables when present:

- `APP_PORT`, default `3218`
- `OLLAMA_BASE_URL`, default `http://host.docker.internal:11434`
- `OLLAMA_MODEL`, default `gemma4:31b-cloud`
- `OLLAMA_THINKING_MODEL`, default `true`
- `WHISPER_BACKEND`, default `auto`
- `WHISPER_BINARY_PATH`
- `WHISPER_MODEL_PATH`
- `WHISPER_PYTHON_BIN`
- `WHISPER_OPENAI_MODEL`, default `base.en`
- `WHISPER_LANGUAGE`, default `en`

Set them in `Settings` -> `Secrets and variables` -> `Actions` -> `Variables`.

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
