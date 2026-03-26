# HyCert Agent - Unified Deployment Script (Windows)
# Usage: Run as Administrator in PowerShell
#   .\deploy-windows.ps1

$ErrorActionPreference = "Stop"

# ─── Configuration ───────────────────────────────────────────────────────────

$AgentDir    = "D:\hycert-agent"
$BinName     = "hycert-agent-windows-amd64.exe"
$BinPath     = "$AgentDir\$BinName"
$ConfigFile  = "$AgentDir\agent.yaml"
$AgentIdFile = "$AgentDir\agent-id"
$BackupDir   = "D:/hycert-agent/backups"
$LogDir      = "$AgentDir\logs"
$LogFile     = "D:/hycert-agent/logs/agent.log"
$ServiceName = "hycert-agent"

# ─── Helpers ─────────────────────────────────────────────────────────────────

function Write-Info { param([string]$msg) Write-Host "  -> $msg" }

function Get-YamlValue {
    param([string]$File, [string]$Key)
    if (Test-Path $File) {
        $match = Select-String -Path $File -Pattern "${Key}:\s*`"?([^`"]+)`"?" -AllMatches
        if ($match.Matches.Count -gt 0) {
            return $match.Matches[0].Groups[1].Value.Trim()
        }
    }
    return ""
}

function Invoke-Api {
    param([string]$Url, [string]$Method = "GET", [hashtable]$Headers = @{}, [string]$Body = "")
    $params = @{
        Uri         = $Url
        Method      = $Method
        ContentType = "application/json"
        Headers     = $Headers
        ErrorAction = "SilentlyContinue"
    }
    if ($Body) { $params.Body = [System.Text.Encoding]::UTF8.GetBytes($Body) }
    try {
        return Invoke-RestMethod @params
    } catch {
        return $null
    }
}

# ─── [1/8] Check prerequisites ───────────────────────────────────────────────

Write-Host "=== [1/8] Check prerequisites ==="
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Please run as Administrator"
    exit 1
}
Write-Info "OK (Administrator)"

# ─── [2/8] Install binary ────────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [2/8] Install binary ==="
if (-not (Test-Path $BinPath)) {
    Write-Error "Binary not found: $BinPath`n  Copy $BinName to $AgentDir first."
    exit 1
}
Write-Info "Binary: $BinPath"
& $BinPath version

# ─── [3/8] Create directories ────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [3/8] Create directories ==="
foreach ($dir in @($LogDir, "$AgentDir\backups")) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    Write-Info $dir
}

# ─── [4/8] Check existing config ─────────────────────────────────────────────

Write-Host ""
Write-Host "=== [4/8] Check existing config ==="
$SkipSetup = $false
$existingUrl = ""
$interval = "3600"

if (Test-Path $ConfigFile) {
    $existingUrl   = Get-YamlValue -File $ConfigFile -Key "url"
    $existingToken = Get-YamlValue -File $ConfigFile -Key "token"
    $existingName  = Get-YamlValue -File $ConfigFile -Key "name"
    $tokenPrefix   = if ($existingToken.Length -ge 20) { $existingToken.Substring(0, 20) } else { $existingToken }

    Write-Host "  Found existing config:"
    Write-Host "    Server URL:  $existingUrl"
    Write-Host "    Token:       ${tokenPrefix}..."
    Write-Host "    Agent Name:  $existingName"
    Write-Host ""

    # Validate token
    if ($existingToken -and $existingToken -ne "hycert_agt_xxxxx...") {
        Write-Info "Validating token..."
        $agentUuid = ""
        if (Test-Path $AgentIdFile) {
            $agentUuid = (Get-Content $AgentIdFile -First 1).Trim()
        }
        if ($agentUuid) {
            $validateResp = Invoke-Api -Url "$existingUrl/api/v1/agent/cert/register" -Method "POST" `
                -Headers @{ "Authorization" = "Bearer $existingToken"; "X-Agent-ID" = $agentUuid } `
                -Body "{`"agent_id`":`"$agentUuid`",`"hostname`":`"$env:COMPUTERNAME`"}"

            if ($validateResp -and $validateResp.success -eq $true) {
                Write-Info "Token is valid"
                Write-Host ""
                Write-Host "  [1] Continue with existing settings (default)"
                Write-Host "  [2] Reconfigure (full setup)"
                $choice = Read-Host "  Choose [1]"
                if (-not $choice) { $choice = "1" }
                if ($choice -eq "1") {
                    $SkipSetup = $true
                }
            } else {
                Write-Info "Token validation failed. Proceeding with full setup."
            }
        } else {
            Write-Info "No agent-id found. Proceeding with full setup."
        }
    }
} else {
    Write-Info "No existing config found. Starting fresh setup."
}

if (-not $SkipSetup) {

# ─── [5/8] Collect settings ──────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [5/8] Collect settings ==="

# Server URL
$defaultUrl = if ($existingUrl) { $existingUrl } else { "" }
$promptUrl = "  Server URL (e.g., https://domain/hycert-api)"
if ($defaultUrl) { $promptUrl += " [$defaultUrl]" }
$serverUrl = Read-Host $promptUrl
if (-not $serverUrl) { $serverUrl = $defaultUrl }
if (-not $serverUrl) { Write-Error "Server URL is required"; exit 1 }

# Proxy
$proxy = Read-Host "  HTTP proxy (empty = none)"

# SSL verify
$skipSsl = Read-Host "  Skip SSL verify? (y/N)"
$insecure = if ($skipSsl -match "^[Yy]") { "true" } else { "false" }

# Tenant code
$tenantCode = Read-Host "  Tenant code [system]"
if (-not $tenantCode) { $tenantCode = "system" }

# Admin credentials
$adminUser = Read-Host "  Admin username [admin]"
if (-not $adminUser) { $adminUser = "admin" }
$adminPass = Read-Host "  Admin password" -AsSecureString
$adminPassPlain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
    [Runtime.InteropServices.Marshal]::SecureStringToBSTR($adminPass))
if (-not $adminPassPlain) { Write-Error "Password is required"; exit 1 }

# Label
$label = Read-Host "  Token label (for grouping, empty = none)"

# Agent name
$defaultName = $env:COMPUTERNAME
$agentName = Read-Host "  Agent display name [$defaultName]"
if (-not $agentName) { $agentName = $defaultName }

# Interval
$interval = Read-Host "  Poll interval in seconds [3600]"
if (-not $interval) { $interval = "3600" }

# ─── [6/8] Login and acquire token ───────────────────────────────────────────

Write-Host ""
Write-Host "=== [6/8] Login and acquire token ==="

# Derive hyadmin URL
$hyadminUrl = $serverUrl -replace '/hycert-api.*$', '/hyadmin-api'
Write-Info "hyadmin URL: $hyadminUrl"

# Verify hyadmin connectivity
$health = Invoke-Api -Url "$hyadminUrl/api/v1/health"
if (-not $health) {
    Write-Host "  Warning: Cannot reach $hyadminUrl/api/v1/health"
    $hyadminUrl = Read-Host "  Enter hyadmin API URL manually"
    if (-not $hyadminUrl) { Write-Error "hyadmin URL is required for login"; exit 1 }
}

# Login
$loginBody = "{`"tenant_code`":`"$tenantCode`",`"username`":`"$adminUser`",`"password`":`"$adminPassPlain`"}"
$loginResp = Invoke-Api -Url "$hyadminUrl/api/v1/auth/login" -Method "POST" -Body $loginBody
if (-not $loginResp -or -not $loginResp.token) {
    Write-Error "Login failed. Check credentials."
    exit 1
}
$jwt = $loginResp.token
Write-Info "Login OK"

# Acquire token
$agentToken = ""

if ($label) {
    Write-Info "Checking existing token for label: $label"
    $labelResp = Invoke-Api -Url "$serverUrl/api/v1/adm/cert/agent-tokens/by-label/$label" `
        -Headers @{ "Authorization" = "Bearer $jwt" }

    if ($labelResp -and $labelResp.success -eq $true -and $labelResp.data.token) {
        $agentToken = $labelResp.data.token
        $tokenPfx = $labelResp.data.token_prefix
        Write-Info "Reusing existing token for label '$label': ${tokenPfx}..."
    }
}

if (-not $agentToken) {
    Write-Info "Creating new token: token-$agentName"
    $createBody = "{`"name`":`"token-$agentName`""
    if ($label) {
        $createBody += ",`"label`":`"$label`""
    }
    $createBody += "}"

    $createResp = Invoke-Api -Url "$serverUrl/api/v1/adm/cert/agent-tokens" -Method "POST" `
        -Headers @{ "Authorization" = "Bearer $jwt"; "Content-Type" = "application/json" } `
        -Body $createBody

    if (-not $createResp -or -not $createResp.data.token) {
        Write-Error "Failed to create token."
        exit 1
    }
    $agentToken = $createResp.data.token
    $tokenDisplay = $agentToken.Substring(0, [Math]::Min(20, $agentToken.Length))
    Write-Info "Token created: ${tokenDisplay}..."
}

# ─── [7/8] Write config ──────────────────────────────────────────────────────

Write-Host ""
Write-Host "=== [7/8] Write config ==="

$proxyLine = if ($proxy) { $proxy } else { "" }

$configContent = @"
server:
  url: "$serverUrl"
  token: "$agentToken"
  proxy: "$proxyLine"
  insecure_skip_verify: $insecure

agent:
  name: "$agentName"
  interval: $interval
  backup: true
  backup_dir: "$BackupDir"

log:
  level: "debug"
  file: "$LogFile"
  max_size: 10
  max_backups: 3
  max_age: 30
  compress: true
"@

$configContent | Out-File -FilePath $ConfigFile -Encoding UTF8 -Force
Write-Info "Config: $ConfigFile"

# Set ACL: only Administrators + SYSTEM can access
try {
    $acl = Get-Acl $AgentDir
    $acl.SetAccessRuleProtection($true, $false)
    $adminRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        "Administrators", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")
    $systemRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        "SYSTEM", "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")
    $acl.SetAccessRule($adminRule)
    $acl.SetAccessRule($systemRule)
    Set-Acl -Path $AgentDir -AclObject $acl
    Write-Info "ACL set: Administrators + SYSTEM only"
} catch {
    Write-Info "Warning: Could not set ACL on $AgentDir"
}

}  # end of if (-not $SkipSetup)

# ─── [8/8] Install and start service ─────────────────────────────────────────

Write-Host ""
Write-Host "=== [8/8] Install and start service ==="

# Stop and uninstall existing service
& $BinPath service stop 2>$null
& $BinPath service uninstall 2>$null
Start-Sleep -Seconds 2

# Install and start
& $BinPath service install --config $ConfigFile
& $BinPath service start
Start-Sleep -Seconds 1
& $BinPath service status

Write-Host ""
Write-Host "Done."
Write-Host "  Binary:   $BinPath"
Write-Host "  Config:   $ConfigFile"
Write-Host "  Service:  $ServiceName (interval=${interval}s)"
Write-Host "  Log:      $LogFile"
Write-Host "  Backup:   $BackupDir"
Write-Host ""
Write-Host "Commands:"
Write-Host "  .\$BinName service status     # Check status"
Write-Host "  .\$BinName service stop       # Stop"
Write-Host "  .\$BinName service start      # Start"
