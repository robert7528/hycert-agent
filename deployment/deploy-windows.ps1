# HyCert Agent - Windows Deployment Script
# Usage: Run as Administrator
#   .\deploy-windows.ps1
#
# Prerequisites:
#   - hycert-agent-windows-amd64.exe in the same directory
#   - NSSM (https://nssm.cc/) — nssm.exe in the same directory or in PATH

$ErrorActionPreference = "Stop"

$AgentDir = "D:\hycert-agent"
$AgentExe = "$AgentDir\hycert-agent-windows-amd64.exe"
$ConfigFile = "$AgentDir\agent.yaml"
$AgentIdFile = "$AgentDir\agent-id"
$ServiceName = "hycert-agent"

# ─── Check prerequisites ─────────────────────────────────────────────────────

if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Please run as Administrator"
    exit 1
}

if (-not (Test-Path $AgentExe)) {
    Write-Error "Agent binary not found: $AgentExe"
    exit 1
}

# ─── [1/6] Stop existing service ─────────────────────────────────────────────

Write-Host ""
Write-Host "=== [1/6] Stop existing service ==="
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing -and $existing.Status -eq 'Running') {
    Write-Host "  -> Stopping $ServiceName..."
    Stop-Service $ServiceName -Force
}

# Show version
& $AgentExe version

# ─── [2/6] Create directories ────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [2/6] Create directories ==="
New-Item -ItemType Directory -Path "$AgentDir\logs" -Force | Out-Null
New-Item -ItemType Directory -Path "$AgentDir\backups" -Force | Out-Null
Write-Host "  -> $AgentDir\logs"
Write-Host "  -> $AgentDir\backups"

# ─── [3/6] Collect settings ──────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [3/6] Collect settings ==="

# Agent name
$defaultName = $env:COMPUTERNAME
$agentName = Read-Host "  Agent display name [$defaultName]"
if (-not $agentName) { $agentName = $defaultName }

# Server URL
$defaultUrl = "https://jumper.k00.com.tw/hycert-api"
$serverUrl = Read-Host "  Server URL [$defaultUrl]"
if (-not $serverUrl) { $serverUrl = $defaultUrl }

# Proxy
$proxy = Read-Host "  HTTP proxy (empty = none)"

# Insecure skip verify
$insecure = Read-Host "  Skip SSL verify? (y/N)"
$insecureVal = if ($insecure -match '^[Yy]') { "true" } else { "false" }

# Token
$existingToken = ""
if (Test-Path $ConfigFile) {
    $match = Select-String -Path $ConfigFile -Pattern 'token:\s*"([^"]+)"' -AllMatches
    if ($match.Matches.Count -gt 0) {
        $existingToken = $match.Matches[0].Groups[1].Value
    }
}

if ($existingToken -and $existingToken -ne "hycert_agt_xxxxx...") {
    Write-Host "  -> Config already has a token, reusing."
    $token = $existingToken
} else {
    $token = Read-Host "  Agent token (hycert_agt_...)"
    if (-not $token) {
        Write-Error "Token is required"
        exit 1
    }
}

# ─── [4/6] Write config ──────────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [4/6] Write config ==="

$proxyLine = if ($proxy) { "  proxy: `"$proxy`"" } else { "  proxy: `"`"" }

$configContent = @"
server:
  url: "$serverUrl"
  token: "$token"
$proxyLine
  insecure_skip_verify: $insecureVal

agent:
  name: "$agentName"
  interval: 3600
  backup: true
  backup_dir: "D:/hycert-agent/backups"

log:
  level: "debug"
  file: "D:/hycert-agent/logs/agent.log"
  max_size: 10
  max_backups: 3
  max_age: 30
  compress: true
"@

Set-Content -Path $ConfigFile -Value $configContent -Encoding UTF8
Write-Host "  -> Config: $ConfigFile"

# ─── [5/6] Install Windows service (NSSM) ────────────────────────────────────

Write-Host ""
Write-Host "=== [5/6] Install Windows service ==="

$nssm = Get-Command nssm -ErrorAction SilentlyContinue
if (-not $nssm) {
    $nssm = Get-Command "$AgentDir\nssm.exe" -ErrorAction SilentlyContinue
}

if ($nssm) {
    $nssmPath = $nssm.Source

    # Remove existing
    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($svc) {
        & $nssmPath stop $ServiceName 2>$null
        & $nssmPath remove $ServiceName confirm 2>$null
    }

    & $nssmPath install $ServiceName $AgentExe "daemon --config $ConfigFile"
    & $nssmPath set $ServiceName DisplayName "HyCert Deployment Agent"
    & $nssmPath set $ServiceName Description "Checks and deploys certificates to this host"
    & $nssmPath set $ServiceName AppDirectory $AgentDir
    & $nssmPath set $ServiceName Start SERVICE_AUTO_START
    & $nssmPath set $ServiceName AppStdout "$AgentDir\logs\service-stdout.log"
    & $nssmPath set $ServiceName AppStderr "$AgentDir\logs\service-stderr.log"
    & $nssmPath set $ServiceName AppRotateFiles 1
    & $nssmPath set $ServiceName AppRotateBytes 10485760

    Write-Host "  -> Service installed: $ServiceName (NSSM)"
} else {
    Write-Host "  !! NSSM not found. Download from https://nssm.cc/download"
    Write-Host "  !! Place nssm.exe in $AgentDir and re-run, or install service manually:"
    Write-Host "  !! nssm install $ServiceName $AgentExe `"daemon --config $ConfigFile`""
    Write-Host ""
}

# ─── [6/6] Test and start ────────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [6/6] Test and start ==="

# Run once to test
Write-Host "  -> Running single test cycle..."
& $AgentExe run --config $ConfigFile

if ($nssm) {
    Write-Host ""
    Write-Host "  -> Starting service..."
    & $nssm.Source start $ServiceName
    Start-Sleep -Seconds 2
    Get-Service -Name $ServiceName | Format-Table Status, Name, DisplayName -AutoSize
}

Write-Host ""
Write-Host "Done."
Write-Host "  Binary:   $AgentExe"
Write-Host "  Config:   $ConfigFile"
Write-Host "  Service:  $ServiceName (auto-start)"
Write-Host "  Log:      $AgentDir\logs\agent.log"
Write-Host "  Backup:   $AgentDir\backups\"
Write-Host ""
Write-Host "Commands:"
Write-Host "  Get-Service $ServiceName              # status"
Write-Host "  Restart-Service $ServiceName           # restart"
Write-Host "  Stop-Service $ServiceName              # stop"
Write-Host "  Get-Content $AgentDir\logs\agent.log -Tail 20  # view log"
