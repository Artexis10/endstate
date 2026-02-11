<#
.SYNOPSIS
    Seeds meaningful WireGuard configuration for curation testing.

.DESCRIPTION
    Sets up WireGuard application configuration with representative non-default values
    WITHOUT creating any tunnel configs or private keys. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - wireguard.exe.config (application settings only)

    DOES NOT configure:
    - Tunnel configuration files (*.conf) - these contain PRIVATE KEYS
    - Any cryptographic material

.EXAMPLE
    .\seed.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Write-Step {
    param([string]$Message)
    Write-Host "[SEED] $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " WireGuard Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$wgDir = Join-Path $env:APPDATA "WireGuard"

if (-not (Test-Path $wgDir)) {
    Write-Step "Creating directory: $wgDir"
    New-Item -ItemType Directory -Path $wgDir -Force | Out-Null
}

# ============================================================================
# WIREGUARD.EXE.CONFIG
# ============================================================================
Write-Step "Writing wireguard.exe.config (app settings only, NO tunnel configs)..."

$configPath = Join-Path $wgDir "wireguard.exe.config"
$config = @'
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <appSettings>
    <add key="LaunchOnLogin" value="true" />
    <add key="AdminTunnelOnly" value="false" />
    <add key="BlockUntunneled" value="false" />
    <add key="ShowNotifications" value="true" />
  </appSettings>
</configuration>
'@
$config | Set-Content -Path $configPath -Encoding UTF8
Write-Pass "wireguard.exe.config written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($configPath)
foreach ($f in $seededFiles) {
    $exists = Test-Path $f
    $size = if ($exists) { (Get-Item $f).Length } else { 'N/A' }
    Write-Host "  $f exists=$exists size=$size" -ForegroundColor Gray
}

# ============================================================================
# SUMMARY
# ============================================================================
Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " WireGuard Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""
Write-Host "[WARN] Tunnel configs (*.conf) are NOT seeded - they contain private keys." -ForegroundColor Magenta
Write-Host "[WARN] WireGuard module marks *.conf as sensitive with warn-only restorer." -ForegroundColor Magenta
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
