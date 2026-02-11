<#
.SYNOPSIS
    Seeds meaningful Plex Media Server configuration for curation testing.

.DESCRIPTION
    Sets up Plex configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Preferences.xml (server preferences - safe values only)

    DOES NOT configure:
    - Authentication tokens (PlexOnlineToken)
    - Media database
    - Cache or transcoding data

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
Write-Host " Plex Media Server Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$plexDir = Join-Path $env:LOCALAPPDATA "Plex Media Server"

if (-not (Test-Path $plexDir)) {
    Write-Step "Creating directory: $plexDir"
    New-Item -ItemType Directory -Path $plexDir -Force | Out-Null
}

# ============================================================================
# PREFERENCES.XML
# ============================================================================
Write-Step "Writing Preferences.xml (safe values only, no tokens)..."

$prefsPath = Join-Path $plexDir "Preferences.xml"
$prefs = @'
<?xml version="1.0" encoding="utf-8"?>
<Preferences
  FriendlyName="Endstate-Server"
  SendCrashReports="0"
  ScanMyLibraryAutomatically="1"
  ScanMyLibraryPeriodically="1"
  ScheduledLibraryUpdateInterval="1800"
  FSEventLibraryUpdatesEnabled="1"
  FSEventLibraryPartialScanEnabled="1"
  LogVerbose="0"
  TranscoderTempDirectory=""
  TranscoderQuality="2"
  TranscoderH264BackgroundPreset="veryfast"
  DlnaEnabled="0"
  GdmEnabled="1"
  ManualPortMappingMode="0"
  ManualPortMappingPort="32400"
  secureConnections="1"
  customCertificatePath=""
  customCertificateKey=""
  customCertificateDomain=""
  ButlerStartHour="2"
  ButlerEndHour="5"
  ButlerTaskDeepMediaAnalysis="1"
  ButlerTaskOptimizeDatabase="1"
  ButlerTaskCleanOldBundles="1"
  ButlerTaskCleanOldCacheFiles="1"
/>
'@
$prefs | Set-Content -Path $prefsPath -Encoding UTF8
Write-Pass "Preferences.xml written (no auth tokens)"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($prefsPath)
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
Write-Host " Plex Media Server Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""
Write-Host "[WARN] Preferences.xml does NOT contain PlexOnlineToken - this is intentional." -ForegroundColor Magenta
Write-Host "[WARN] Real Preferences.xml will contain auth tokens marked sensitive in module.jsonc." -ForegroundColor Magenta
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
