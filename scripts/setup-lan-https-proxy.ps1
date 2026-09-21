param(
  [string]$HostIp = "",
  [int]$AppPort = $(if ($env:APP_PORT) { [int]$env:APP_PORT } else { 3218 }),
  [int]$ApiPort = $(if ($env:API_PORT) { [int]$env:API_PORT } else { 3219 }),
  [int]$HttpsPort = $(if ($env:HTTPS_PORT) { [int]$env:HTTPS_PORT } else { 3443 }),
  [int]$ApiHttpsPort = $(if ($env:API_HTTPS_PORT) { [int]$env:API_HTTPS_PORT } else { 3444 }),
  [string]$UploadsHostDir = "",
  [switch]$SkipCertificateGeneration,
  [switch]$SkipDockerComposeUp
)

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$HttpsDir = Join-Path $ProjectRoot "lan-https"
$CertDir = Join-Path $HttpsDir "certs"

if ([string]::IsNullOrWhiteSpace($UploadsHostDir)) {
  if (-not [string]::IsNullOrWhiteSpace($env:UPLOADS_HOST_DIR)) {
    $UploadsHostDir = $env:UPLOADS_HOST_DIR
  } else {
    $UploadsHostDir = Join-Path $ProjectRoot ".data\uploads"
  }
}

function Get-PrivateIPv4Address {
  $physicalInterfaces = @(Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Select-Object -ExpandProperty InterfaceIndex)
  # Keep an array even when only one address is found, so [0] returns the full IP.
  $addresses = @(Get-NetIPAddress -AddressFamily IPv4 |
    Where-Object {
      $_.IPAddress -match "^(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)" -and
      $_.IPAddress -ne "127.0.0.1"
    } |
    Sort-Object {
      if ($physicalInterfaces -contains $_.InterfaceIndex) { 0 } else { 1 }
    }, {
      if ($_.IPAddress -match "^192\.168\.") {
        0
      } elseif ($_.IPAddress -match "^10\.") {
        1
      } else {
        2
      }
    }, IPAddress |
    Select-Object -ExpandProperty IPAddress -Unique)

  if ($addresses.Count -eq 0) {
    throw "Cannot detect LAN IPv4 address. Run ipconfig and pass -HostIp <windows-ipv4>."
  }

  if ($addresses.Count -gt 1) {
    Write-Host "Detected several LAN IPv4 addresses:"
    $addresses | ForEach-Object { Write-Host "  $_" }
    Write-Host "Using preferred LAN address: $($addresses[0])"
  }

  return $addresses[0]
}

function Update-CurrentProcessPath {
  $machinePath = [System.Environment]::GetEnvironmentVariable("Path", "Machine")
  $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
  $env:Path = "$machinePath;$userPath"
}

function Install-MkcertIfMissing {
  $mkcert = Get-Command mkcert -ErrorAction SilentlyContinue
  if ($mkcert) {
    return
  }

  $winget = Get-Command winget -ErrorAction SilentlyContinue
  if (-not $winget) {
    throw "mkcert is not installed and winget is not available on this runner. Install mkcert or add it to the runner image."
  }

  winget install --id FiloSottile.mkcert -e --accept-package-agreements --accept-source-agreements
  Update-CurrentProcessPath

  $mkcert = Get-Command mkcert -ErrorAction SilentlyContinue
  if (-not $mkcert) {
    throw "mkcert install finished but mkcert is still not on PATH. Restart the runner service or add mkcert to PATH."
  }
}

function Ensure-FirewallRule {
  param([int]$Port)

  $displayName = "Daily Speaking HTTPS $Port"
  $existingRule = Get-NetFirewallRule -DisplayName $displayName -ErrorAction SilentlyContinue
  if ($existingRule) {
    return
  }

  try {
    New-NetFirewallRule -DisplayName $displayName -Direction Inbound -Protocol TCP -LocalPort $Port -Action Allow | Out-Null
  } catch {
    Write-Warning "Could not create Windows Firewall rule '$displayName': $($_.Exception.Message)"
    Write-Warning "Deploy will continue, but LAN clients may be blocked until you pre-create the same rule from an elevated PowerShell."
  }
}

if ([string]::IsNullOrWhiteSpace($HostIp)) {
  $HostIp = Get-PrivateIPv4Address
}

if ([string]::IsNullOrWhiteSpace($env:COMPOSE_PROJECT_NAME)) {
  $env:COMPOSE_PROJECT_NAME = "daily-speaking"
}

$docker = Get-Command docker -ErrorAction SilentlyContinue
if (-not $docker) {
  throw "Docker is not installed or is not on PATH. Start Docker Desktop and make sure 'docker compose version' works."
}

New-Item -ItemType Directory -Force $CertDir | Out-Null
New-Item -ItemType Directory -Force $UploadsHostDir | Out-Null
$env:UPLOADS_HOST_DIR = $UploadsHostDir
$env:UPLOADS_DIR = "/app/uploads"
Push-Location $ProjectRoot

try {
  $certPath = Join-Path $CertDir "daily-speaking.pem"
  $keyPath = Join-Path $CertDir "daily-speaking-key.pem"
  $caddyfilePath = Join-Path $HttpsDir "Caddyfile"

  if (-not $SkipCertificateGeneration) {
    Install-MkcertIfMissing
    mkcert -install
    mkcert -cert-file $certPath -key-file $keyPath $HostIp
  }

  $caddyfile = @"
https://${HostIp}:${HttpsPort} {
  tls /certs/daily-speaking.pem /certs/daily-speaking-key.pem
  reverse_proxy web:3000
}

https://${HostIp}:${ApiHttpsPort} {
  tls /certs/daily-speaking.pem /certs/daily-speaking-key.pem
  reverse_proxy backend:3000
}
"@

  Set-Content -Path $caddyfilePath -Value $caddyfile -Encoding ascii

  Ensure-FirewallRule -Port $HttpsPort
  Ensure-FirewallRule -Port $ApiHttpsPort

  $env:APP_PORT = "$AppPort"
  $env:API_PORT = "$ApiPort"
  $env:HTTPS_PORT = "$HttpsPort"
  $env:API_HTTPS_PORT = "$ApiHttpsPort"
  $env:PUBLIC_API_BASE_URL = "https://${HostIp}:${ApiHttpsPort}"
  $env:CORS_ALLOWED_ORIGINS = "https://${HostIp}:${HttpsPort},https://${HostIp}:${ApiHttpsPort},http://${HostIp}:${AppPort},http://${HostIp}:${ApiPort}"
  $env:SESSION_COOKIE_SECURE = "true"
  $env:SESSION_COOKIE_SAME_SITE = "lax"

  if (-not [string]::IsNullOrWhiteSpace($env:GITHUB_ENV)) {
    "LAN_HOST_IP=$HostIp" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
  }

  if (-not $SkipDockerComposeUp) {
    docker compose up --build -d --remove-orphans web backend postgres lan-https
  }

  Write-Host ""
  Write-Host "LAN HTTPS Docker proxy files are ready:"
  Write-Host "  $HttpsDir"
  Write-Host "Uploaded media storage:"
  Write-Host "  $UploadsHostDir"
  Write-Host ""
  Write-Host "Allow both HTTPS ports in an elevated PowerShell once:"
  Write-Host "  New-NetFirewallRule -DisplayName `"Daily Speaking HTTPS $HttpsPort`" -Direction Inbound -Protocol TCP -LocalPort $HttpsPort -Action Allow"
  Write-Host "  New-NetFirewallRule -DisplayName `"Daily Speaking HTTPS $ApiHttpsPort`" -Direction Inbound -Protocol TCP -LocalPort $ApiHttpsPort -Action Allow"
  Write-Host ""
  Write-Host "Start or restart the Docker services with HTTPS:"
  Write-Host "  cd `"$ProjectRoot`""
  Write-Host "  .\scripts\setup-lan-https-proxy.ps1 -HostIp $HostIp -AppPort $AppPort -ApiPort $ApiPort -HttpsPort $HttpsPort -ApiHttpsPort $ApiHttpsPort -UploadsHostDir `"$UploadsHostDir`" -SkipCertificateGeneration"
  Write-Host ""
  Write-Host "Open from another LAN device:"
  Write-Host "  Web:     https://${HostIp}:${HttpsPort}"
  Write-Host "  API:     https://${HostIp}:${ApiHttpsPort}"
  Write-Host "  Health:  https://${HostIp}:${ApiHttpsPort}/healthz"
  Write-Host "  Swagger: https://${HostIp}:${ApiHttpsPort}/docs"
  Write-Host "Use the HTTPS web URL for browser microphone recording."
  Write-Host ""
  Write-Host "If another machine warns about the certificate, import mkcert rootCA.pem there."
  Write-Host "Find it on this Windows host with:"
  Write-Host "  mkcert -CAROOT"
} finally {
  Pop-Location
}
