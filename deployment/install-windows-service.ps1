# HyCert Agent - Windows Service Installation
# Usage: Run as Administrator
#   .\install-windows-service.ps1
#
# Prerequisites:
#   - NSSM (https://nssm.cc/) installed and in PATH, or place nssm.exe next to this script
#   - D:\hycert-agent\hycert-agent-windows-amd64.exe
#   - D:\hycert-agent\agent.yaml configured

$ServiceName = "hycert-agent"
$AgentDir = "D:\hycert-agent"
$AgentExe = "$AgentDir\hycert-agent-windows-amd64.exe"
$ConfigFile = "$AgentDir\agent.yaml"

# ─── Check prerequisites ─────────────────────────────────────────────────────

if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Please run as Administrator"
    exit 1
}

if (-not (Test-Path $AgentExe)) {
    Write-Error "Agent binary not found: $AgentExe"
    exit 1
}

if (-not (Test-Path $ConfigFile)) {
    Write-Error "Config not found: $ConfigFile"
    exit 1
}

# Try to find NSSM
$nssm = Get-Command nssm -ErrorAction SilentlyContinue
if (-not $nssm) {
    $nssm = Get-Command "$AgentDir\nssm.exe" -ErrorAction SilentlyContinue
}
if (-not $nssm) {
    Write-Host ""
    Write-Host "NSSM not found. Please download from https://nssm.cc/download"
    Write-Host "and place nssm.exe in $AgentDir or add to PATH."
    Write-Host ""
    exit 1
}
$nssmPath = $nssm.Source

# ─── Install service ─────────────────────────────────────────────────────────

# Remove existing service if present
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Stopping existing service..."
    & $nssmPath stop $ServiceName 2>$null
    & $nssmPath remove $ServiceName confirm
}

Write-Host "Installing service: $ServiceName"
& $nssmPath install $ServiceName $AgentExe "daemon --config $ConfigFile"

# Configure service
& $nssmPath set $ServiceName DisplayName "HyCert Deployment Agent"
& $nssmPath set $ServiceName Description "Checks and deploys certificates to this host"
& $nssmPath set $ServiceName AppDirectory $AgentDir
& $nssmPath set $ServiceName Start SERVICE_AUTO_START
& $nssmPath set $ServiceName AppStdout "$AgentDir\logs\service-stdout.log"
& $nssmPath set $ServiceName AppStderr "$AgentDir\logs\service-stderr.log"
& $nssmPath set $ServiceName AppRotateFiles 1
& $nssmPath set $ServiceName AppRotateBytes 10485760

# Create directories
New-Item -ItemType Directory -Path "$AgentDir\logs" -Force | Out-Null
New-Item -ItemType Directory -Path "$AgentDir\backups" -Force | Out-Null

# Start service
Write-Host "Starting service..."
& $nssmPath start $ServiceName

# Show status
Write-Host ""
Get-Service -Name $ServiceName | Format-Table Status, Name, DisplayName -AutoSize

Write-Host ""
Write-Host "Done."
Write-Host "  Binary:  $AgentExe"
Write-Host "  Config:  $ConfigFile"
Write-Host "  Service: $ServiceName (auto-start)"
Write-Host ""
Write-Host "Commands:"
Write-Host "  Get-Service $ServiceName              # status"
Write-Host "  Restart-Service $ServiceName           # restart"
Write-Host "  Stop-Service $ServiceName              # stop"
Write-Host "  nssm edit $ServiceName                 # edit settings (GUI)"
