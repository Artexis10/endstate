<#
.SYNOPSIS
    Seeds meaningful EVGA Precision X1 configuration for curation testing.

.DESCRIPTION
    Sets up EVGA Precision X1 configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Profiles/ (GPU tuning profiles)
    - Settings.xml (application settings)

    DOES NOT configure:
    - License information

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
Write-Host " EVGA Precision X1 Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$evgaDir = Join-Path $env:LOCALAPPDATA "EVGA\Precision X1"
$profilesDir = Join-Path $evgaDir "Profiles"

foreach ($dir in @($evgaDir, $profilesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# SETTINGS.XML
# ============================================================================
Write-Step "Writing Settings.xml..."

$settingsPath = Join-Path $evgaDir "Settings.xml"
$settings = @'
<?xml version="1.0" encoding="UTF-8"?>
<PrecisionX1Settings>
  <General>
    <StartWithWindows>false</StartWithWindows>
    <StartMinimized>true</StartMinimized>
    <ShowOSD>true</ShowOSD>
    <TemperatureUnit>Celsius</TemperatureUnit>
    <UpdateCheck>true</UpdateCheck>
  </General>
  <OSD>
    <ShowGPUTemp>true</ShowGPUTemp>
    <ShowGPUClock>true</ShowGPUClock>
    <ShowMemoryClock>true</ShowMemoryClock>
    <ShowFanSpeed>true</ShowFanSpeed>
    <ShowFPS>true</ShowFPS>
    <Position>TopLeft</Position>
    <FontSize>12</FontSize>
  </OSD>
  <FanControl>
    <AutoFanMode>true</AutoFanMode>
    <FanCurve>
      <Point temp="30" speed="30" />
      <Point temp="50" speed="45" />
      <Point temp="70" speed="65" />
      <Point temp="80" speed="80" />
      <Point temp="90" speed="100" />
    </FanCurve>
  </FanControl>
</PrecisionX1Settings>
'@
$settings | Set-Content -Path $settingsPath -Encoding UTF8
Write-Pass "Settings.xml written"

# ============================================================================
# PROFILES
# ============================================================================
Write-Step "Writing Profiles/Gaming.xml..."

$profile1Path = Join-Path $profilesDir "Gaming.xml"
$profile1 = @'
<?xml version="1.0" encoding="UTF-8"?>
<Profile name="Gaming">
  <GPUClockOffset>100</GPUClockOffset>
  <MemoryClockOffset>200</MemoryClockOffset>
  <PowerLimit>110</PowerLimit>
  <TempLimit>83</TempLimit>
  <FanMode>Auto</FanMode>
  <VoltageOffset>0</VoltageOffset>
</Profile>
'@
$profile1 | Set-Content -Path $profile1Path -Encoding UTF8
Write-Pass "Gaming profile written"

Write-Step "Writing Profiles/Silent.xml..."

$profile2Path = Join-Path $profilesDir "Silent.xml"
$profile2 = @'
<?xml version="1.0" encoding="UTF-8"?>
<Profile name="Silent">
  <GPUClockOffset>-100</GPUClockOffset>
  <MemoryClockOffset>0</MemoryClockOffset>
  <PowerLimit>80</PowerLimit>
  <TempLimit>75</TempLimit>
  <FanMode>Custom</FanMode>
  <VoltageOffset>-50</VoltageOffset>
</Profile>
'@
$profile2 | Set-Content -Path $profile2Path -Encoding UTF8
Write-Pass "Silent profile written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath, $profile1Path, $profile2Path)
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
Write-Host " EVGA Precision X1 Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
