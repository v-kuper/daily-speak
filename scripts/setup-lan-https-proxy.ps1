param(
  [string]$HostIp = "",
  [int]$HttpsPort = $(if ($env:HTTPS_PORT) { [int]$env:HTTPS_PORT } else { 3443 }),
  [switch]$SkipCertificateGeneration,
  [switch]$SkipDockerComposeUp
)

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$HttpsDir = Join-Path $ProjectRoot "lan-https"
$CertDir = Join-Path $HttpsDir "certs"

function Get-PrivateIPv4Address {
  $addresses = Get-NetIPAddress -AddressFamily IPv4 |
    Where-Object {
      $_.IPAddress -match "^(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)" -and
      $_.IPAddress -ne "127.0.0.1"
    } |
    Select-Object -ExpandProperty IPAddress -Unique

  if ($addresses.Count -eq 0) {
    throw "Cannot detect LAN IPv4 address. Run ipconfig and pass -HostIp <windows-ipv4>."
  }

  if ($addresses.Count -gt 1) {
    Write-Host "Detected several LAN IPv4 addresses:"
    $addresses | ForEach-Object { Write-Host "  $_" }
    Write-Host "Using first one: $($addresses[0])"
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
  $displayName = "Daily Speaking HTTPS $HttpsPort"
  $existingRule = Get-NetFirewallRule -DisplayName $displayName -ErrorAction SilentlyContinue
  if ($existingRule) {
    return
  }

  New-NetFirewallRule -DisplayName $displayName -Direction Inbound -Protocol TCP -LocalPort $HttpsPort -Action Allow | Out-Null
}

if ([string]::IsNullOrWhiteSpace($HostIp)) {
  $HostIp = Get-PrivateIPv4Address
}

$docker = Get-Command docker -ErrorAction SilentlyContinue
if (-not $docker) {
  throw "Docker is not installed or is not on PATH. Start Docker Desktop and make sure 'docker compose version' works."
}

New-Item -ItemType Directory -Force $CertDir | Out-Null
Push-Location $ProjectRoot

try {
  $certPath = Join-Path $CertDir "daily-speaking.pem"
  $keyPath = Join-Path $CertDir "daily-speaking-key.pem"
  $caddyfilePath = Join-Path $HttpsDir "Caddyfile"

  if (-not $SkipCertificateGeneration) {
    Install-MkcertIfMissing
    mkcert -install
    mkcert -cert-file $certPath -key-file $keyPath $HostIp localhost 127.0.0.1
  }

  $caddyfile = @"
https://${HostIp}:${HttpsPort}, https://localhost:${HttpsPort}, https://127.0.0.1:${HttpsPort} {
  tls /certs/daily-speaking.pem /certs/daily-speaking-key.pem
  reverse_proxy app:3000
}
"@

  Set-Content -Path $caddyfilePath -Value $caddyfile -Encoding ascii

  Ensure-FirewallRule

  if (-not $SkipDockerComposeUp) {
    $env:HTTPS_PORT = "$HttpsPort"
    docker compose up --build -d app postgres lan-https
  }

  Write-Host ""
  Write-Host "LAN HTTPS Docker proxy files are ready:"
  Write-Host "  $HttpsDir"
  Write-Host ""
  Write-Host "Allow the HTTPS port in an elevated PowerShell once:"
  Write-Host "  New-NetFirewallRule -DisplayName `"Daily Speaking HTTPS $HttpsPort`" -Direction Inbound -Protocol TCP -LocalPort $HttpsPort -Action Allow"
  Write-Host ""
  Write-Host "Start or restart the Docker app with HTTPS:"
  Write-Host "  cd `"$ProjectRoot`""
  Write-Host "  `$env:HTTPS_PORT=$HttpsPort; docker compose up --build -d app postgres lan-https"
  Write-Host ""
  Write-Host "Open from another LAN device:"
  Write-Host "  https://${HostIp}:${HttpsPort}"
  Write-Host ""
  Write-Host "If another machine warns about the certificate, import mkcert rootCA.pem there."
  Write-Host "Find it on this Windows host with:"
  Write-Host "  mkcert -CAROOT"
} finally {
  Pop-Location
}
