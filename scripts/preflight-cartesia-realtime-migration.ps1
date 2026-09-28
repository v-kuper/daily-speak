param(
  [switch]$QuiesceBackend
)

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$QuiescedBackendVariable = "DAILY_SPEAKING_QUIESCED_BACKEND_ID"
$MigrationName = "0014_cartesia_realtime_transcripts.sql"

if ([string]::IsNullOrWhiteSpace($env:COMPOSE_PROJECT_NAME)) {
  $env:COMPOSE_PROJECT_NAME = "daily-speaking"
}

function Get-ComposeContainerId {
  param([string]$Service)

  $containerIds = @(& docker compose ps -a -q $Service 2>$null)
  if ($LASTEXITCODE -ne 0) {
    throw "Could not inspect the existing Docker Compose stack. Verify that Docker Desktop is running."
  }
  return (($containerIds | Select-Object -First 1) | Out-String).Trim()
}

function Test-ContainerRunning {
  param([string]$ContainerId)

  if ([string]::IsNullOrWhiteSpace($ContainerId)) {
    return $false
  }
  $state = (& docker inspect --format "{{.State.Running}}" $ContainerId 2>$null | Out-String).Trim()
  if ($LASTEXITCODE -ne 0) {
    return $false
  }
  return $state -eq "true"
}

function Set-QuiescedBackendMarker {
  param([string]$ContainerId)

  $env:DAILY_SPEAKING_QUIESCED_BACKEND_ID = $ContainerId
  if (-not [string]::IsNullOrWhiteSpace($env:GITHUB_ENV)) {
    "$QuiescedBackendVariable=$ContainerId" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
  }
}

function Clear-QuiescedBackendMarker {
  $env:DAILY_SPEAKING_QUIESCED_BACKEND_ID = ""
  if (-not [string]::IsNullOrWhiteSpace($env:GITHUB_ENV)) {
    "$QuiescedBackendVariable=" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
  }
}

function Get-ExistingPostgresContainer {
  $containerId = Get-ComposeContainerId -Service "postgres"
  if ([string]::IsNullOrWhiteSpace($containerId)) {
    $volumes = @(& docker volume ls --quiet `
      --filter "label=com.docker.compose.project=$env:COMPOSE_PROJECT_NAME" `
      --filter "label=com.docker.compose.volume=postgres_data" 2>$null)
    if ($LASTEXITCODE -ne 0) {
      throw "Could not inspect the existing PostgreSQL volume. Verify that Docker Desktop is running."
    }
    if ($volumes.Count -eq 0) {
      return ""
    }

    Write-Host "Existing PostgreSQL data volume found; starting only the database for the rollout check."
    & docker compose up -d --no-deps postgres | Out-Host
    if ($LASTEXITCODE -ne 0) {
      throw "Could not start the existing PostgreSQL service for the rollout check."
    }
    $containerId = Get-ComposeContainerId -Service "postgres"
  } elseif (-not (Test-ContainerRunning -ContainerId $containerId)) {
    Write-Host "Starting the existing PostgreSQL container for the rollout check."
    & docker start $containerId | Out-Null
    if ($LASTEXITCODE -ne 0) {
      throw "Could not start the existing PostgreSQL container for the rollout check."
    }
  }

  if ([string]::IsNullOrWhiteSpace($containerId)) {
    throw "The PostgreSQL data volume exists, but its Compose service could not be located."
  }
  for ($attempt = 1; $attempt -le 30; $attempt++) {
    & docker exec $containerId pg_isready -q -U postgres -d daily_speaking *> $null
    if ($LASTEXITCODE -eq 0) {
      return $containerId
    }
    Start-Sleep -Seconds 2
  }
  throw "The existing PostgreSQL service did not become ready within 60 seconds. The application stack was not replaced."
}

function Invoke-PostgresScalar {
  param(
    [string]$ContainerId,
    [string]$Sql
  )

  $output = @(& docker exec $ContainerId psql -X -v "ON_ERROR_STOP=1" `
    -U postgres -d daily_speaking -tA -c $Sql 2>$null)
  if ($LASTEXITCODE -ne 0) {
    throw "Could not inspect migration readiness in the existing PostgreSQL database. The application stack was not replaced."
  }
  return (($output | Select-Object -First 1) | Out-String).Trim()
}

$LegacyJobPredicate = @"
job.state IN ('queued', 'running', 'retry_wait')
AND (
  (
    job.kind = 'recording.process'
    AND EXISTS (
      SELECT 1 FROM interview_sessions session
      WHERE session.recording_id = job.resource_id
    )
  )
  OR (
    job.kind = 'guest.preview'
    AND EXISTS (
      SELECT 1 FROM interview_sessions session
      WHERE session.guest_preview_id = job.resource_id
    )
  )
)
"@

function Assert-NoActiveLegacyInterviewJobs {
  param([string]$PostgresContainerId)

  $countText = Invoke-PostgresScalar -ContainerId $PostgresContainerId -Sql @"
SELECT COUNT(*)
FROM processing_jobs job
WHERE $LegacyJobPredicate;
"@
  $count = 0
  if (-not [int]::TryParse($countText, [ref]$count)) {
    throw "PostgreSQL returned an invalid migration-readiness result. The application stack was not replaced."
  }
  if ($count -eq 0) {
    return
  }

  $summary = Invoke-PostgresScalar -ContainerId $PostgresContainerId -Sql @"
SELECT COALESCE(
  string_agg(active.kind || '/' || active.state || '=' || active.total, ', ' ORDER BY active.kind, active.state),
  'unknown'
)
FROM (
  SELECT job.kind, job.state, COUNT(*)::text AS total
  FROM processing_jobs job
  WHERE $LegacyJobPredicate
  GROUP BY job.kind, job.state
) active;
"@
  throw "Cartesia realtime rollout blocked: $count active legacy interview finalization job(s) remain ($summary). The existing application stack was not replaced. Keep the old worker running, inspect 'docker compose logs worker', wait for these jobs to finish, and rerun the deployment."
}

Push-Location $ProjectRoot
try {
  $postgresContainerId = Get-ExistingPostgresContainer
  if ([string]::IsNullOrWhiteSpace($postgresContainerId)) {
    Write-Host "No existing PostgreSQL deployment found; the Cartesia realtime rollout preflight is not needed."
    return
  }

  $catalogState = Invoke-PostgresScalar -ContainerId $postgresContainerId -Sql @"
SELECT concat(
  (to_regclass('public.schema_migrations') IS NOT NULL)::int, ':',
  (to_regclass('public.processing_jobs') IS NOT NULL)::int, ':',
  (to_regclass('public.interview_sessions') IS NOT NULL)::int
);
"@
  if ($catalogState -ne "1:1:1") {
    Write-Host "The existing database predates adaptive interview jobs; no legacy interview finalization work can block this rollout."
    return
  }

  $migrationApplied = Invoke-PostgresScalar -ContainerId $postgresContainerId -Sql @"
SELECT EXISTS (
  SELECT 1 FROM schema_migrations WHERE name = '$MigrationName'
)::int;
"@
  if ($migrationApplied -eq "1") {
    Write-Host "Migration $MigrationName is already applied; no legacy rollout gate is required."
    return
  }

  Assert-NoActiveLegacyInterviewJobs -PostgresContainerId $postgresContainerId
  if (-not $QuiesceBackend) {
    Write-Host "Cartesia realtime migration preflight passed; the existing application stack remains online."
    return
  }

  $backendContainerId = Get-ComposeContainerId -Service "backend"
  $backendWasRunning = Test-ContainerRunning -ContainerId $backendContainerId
  try {
    if ($backendWasRunning) {
      Set-QuiescedBackendMarker -ContainerId $backendContainerId
      Write-Host "Pausing the existing API before the final migration-readiness check; the old worker remains running."
      & docker stop --time 30 $backendContainerId | Out-Null
      if ($LASTEXITCODE -ne 0) {
        throw "Could not pause the existing API for the final migration-readiness check."
      }
    }

    Assert-NoActiveLegacyInterviewJobs -PostgresContainerId $postgresContainerId
  } catch {
    $preflightFailure = $_.Exception.Message
    if ($backendWasRunning -and (Test-ContainerRunning -ContainerId $backendContainerId) -eq $false) {
      & docker start $backendContainerId | Out-Null
      if ($LASTEXITCODE -ne 0) {
        throw "$preflightFailure Automatic recovery also failed; restart the previous backend container before accepting traffic."
      }
      Write-Host "The previous API container was restarted because the rollout gate did not pass."
    }
    Clear-QuiescedBackendMarker
    throw $preflightFailure
  }

  Write-Host "Final Cartesia realtime migration gate passed. The old API is paused and the old worker may now be replaced."
} finally {
  Pop-Location
}
