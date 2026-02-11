<#
.SYNOPSIS
    Seeds meaningful OpenRGB configuration for curation testing.

.DESCRIPTION
    Sets up OpenRGB configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - OpenRGB.json (main configuration)
    - profiles/ (lighting profiles)

    DOES NOT configure:
    - Device-specific hardware addresses

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
Write-Host " OpenRGB Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$openrgbDir = Join-Path $env:APPDATA "OpenRGB"
$profilesDir = Join-Path $openrgbDir "profiles"

foreach ($dir in @($openrgbDir, $profilesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# OpenRGB.json
# ============================================================================
Write-Step "Writing OpenRGB.json..."

$configPath = Join-Path $openrgbDir "OpenRGB.json"
$config = @{
    "AutoStart" = $false
    "MinimizeOnClose" = $true
    "StartMinimized" = $false
    "Theme" = "dark"
    "UserInterface" = @{
        "ShowDeviceView" = $true
        "ShowSDKServer" = $false
        "ShowProfileControls" = $true
    }
    "SDKServer" = @{
        "Enabled" = $false
        "Port" = 6742
        "Host" = "0.0.0.0"
    }
    "Plugins" = @{
        "Enabled" = $true
        "PluginDirectory" = "plugins"
    }
}
$configJson = $config | ConvertTo-Json -Depth 5
$configJson | Set-Content -Path $configPath -Encoding UTF8
Write-Pass "OpenRGB.json written"

# ============================================================================
# PROFILES
# ============================================================================
Write-Step "Writing profiles/Gaming.orp..."

$profile1Path = Join-Path $profilesDir "Gaming.orp"
$profile1 = @'
{
  "name": "Gaming",
  "devices": [
    {
      "name": "Generic RGB",
      "mode": "Static",
      "colors": ["#FF0000", "#00FF00", "#0000FF"],
      "speed": 50,
      "brightness": 100
    }
  ]
}
'@
$profile1 | Set-Content -Path $profile1Path -Encoding UTF8
Write-Pass "Gaming profile written"

Write-Step "Writing profiles/Ambient.orp..."

$profile2Path = Join-Path $profilesDir "Ambient.orp"
$profile2 = @'
{
  "name": "Ambient",
  "devices": [
    {
      "name": "Generic RGB",
      "mode": "Breathing",
      "colors": ["#6A0DAD", "#1E90FF"],
      "speed": 30,
      "brightness": 60
    }
  ]
}
'@
$profile2 | Set-Content -Path $profile2Path -Encoding UTF8
Write-Pass "Ambient profile written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($configPath, $profile1Path, $profile2Path)
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
Write-Host " OpenRGB Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
