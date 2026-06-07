param(
  [string]$HostIp = "",
  [int]$AppPort = $(if ($env:APP_PORT) { [int]$env:APP_PORT } else { 3218 }),
  [int]$HttpsPort = $(if ($env:HTTPS_PORT) { [int]$env:HTTPS_PORT } else { 3443 }),
  [string]$ProxyDir = "",
  [switch]$SkipCertificateGeneration
)

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$ProjectName = Split-Path -Leaf $ProjectRoot

if ([string]::IsNullOrWhiteSpace($ProxyDir)) {
  $ProxyDir = Join-Path (Split-Path -Parent $ProjectRoot) "$($ProjectName)-proxy"
}

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

if ([string]::IsNullOrWhiteSpace($HostIp)) {
  $HostIp = Get-PrivateIPv4Address
}

$mkcert = Get-Command mkcert -ErrorAction SilentlyContinue
if (-not $mkcert) {
  throw "mkcert is not installed. Install it first, for example: winget install FiloSottile.mkcert"
}

$caddy = Get-Command caddy -ErrorAction SilentlyContinue
if (-not $caddy) {
  throw "Caddy is not installed. Install it first, for example: winget install CaddyServer.Caddy"
}

New-Item -ItemType Directory -Force $ProxyDir | Out-Null
Push-Location $ProxyDir

try {
  if (-not $SkipCertificateGeneration) {
    mkcert -install
    mkcert -cert-file daily-speaking.pem -key-file daily-speaking-key.pem $HostIp localhost 127.0.0.1
  }

  $certPath = Join-Path $ProxyDir "daily-speaking.pem"
  $keyPath = Join-Path $ProxyDir "daily-speaking-key.pem"
  $caddyfilePath = Join-Path $ProxyDir "Caddyfile"

  $caddyfile = @"
https://${HostIp}:${HttpsPort} {
  tls "$certPath" "$keyPath"
  reverse_proxy 127.0.0.1:$AppPort
}
"@

  Set-Content -Path $caddyfilePath -Value $caddyfile -Encoding ascii

  Write-Host ""
  Write-Host "LAN HTTPS proxy files are ready:"
  Write-Host "  $ProxyDir"
  Write-Host ""
  Write-Host "Allow the HTTPS port in an elevated PowerShell once:"
  Write-Host "  New-NetFirewallRule -DisplayName `"Daily Speaking HTTPS $HttpsPort`" -Direction Inbound -Protocol TCP -LocalPort $HttpsPort -Action Allow"
  Write-Host ""
  Write-Host "Start the proxy from this PowerShell:"
  Write-Host "  cd `"$ProxyDir`""
  Write-Host "  caddy run --config Caddyfile"
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
