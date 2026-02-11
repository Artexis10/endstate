<#
.SYNOPSIS
    Seeds meaningful FastRawViewer configuration for curation testing.

.DESCRIPTION
    Sets up FastRawViewer configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Settings.xml (application settings)
    - Presets/ (user presets)

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
Write-Host " FastRawViewer Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$frvDir = Join-Path $env:APPDATA "FastRawViewer"
$presetsDir = Join-Path $frvDir "Presets"

foreach ($dir in @($frvDir, $presetsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# SETTINGS.XML
# ============================================================================
Write-Step "Writing Settings.xml..."

$settingsPath = Join-Path $frvDir "Settings.xml"
$settings = @'
<?xml version="1.0" encoding="UTF-8"?>
<FastRawViewer>
  <Display>
    <BackgroundColor>50 50 50</BackgroundColor>
    <ZoomMode>FitToWindow</ZoomMode>
    <ShowHistogram>true</ShowHistogram>
    <HistogramPosition>BottomRight</HistogramPosition>
    <ShowEXIF>true</ShowEXIF>
    <ShowOverexposure>true</ShowOverexposure>
    <ShowUnderexposure>true</ShowUnderexposure>
    <OverexposureThreshold>252</OverexposureThreshold>
    <UnderexposureThreshold>4</UnderexposureThreshold>
  </Display>
  <RAW>
    <WhiteBalance>AsShot</WhiteBalance>
    <Exposure>0.0</Exposure>
    <HalfSize>false</HalfSize>
    <UseEmbeddedJPEG>false</UseEmbeddedJPEG>
  </RAW>
  <FileBrowser>
    <SortOrder>ByName</SortOrder>
    <ShowRatings>true</ShowRatings>
    <ShowLabels>true</ShowLabels>
    <ThumbnailSize>Medium</ThumbnailSize>
  </FileBrowser>
  <XMP>
    <WriteXMPSidecar>true</WriteXMPSidecar>
    <PreserveExistingXMP>true</PreserveExistingXMP>
  </XMP>
</FastRawViewer>
'@
$settings | Set-Content -Path $settingsPath -Encoding UTF8
Write-Pass "Settings.xml written"

# ============================================================================
# PRESETS
# ============================================================================
Write-Step "Writing Presets/Landscape.xml..."

$presetPath = Join-Path $presetsDir "Landscape.xml"
$preset = @'
<?xml version="1.0" encoding="UTF-8"?>
<Preset name="Landscape">
  <Exposure>0.5</Exposure>
  <Contrast>10</Contrast>
  <Shadows>20</Shadows>
  <Highlights>-15</Highlights>
  <WhiteBalance>Daylight</WhiteBalance>
</Preset>
'@
$preset | Set-Content -Path $presetPath -Encoding UTF8
Write-Pass "Landscape preset written"

Write-Step "Writing Presets/Portrait.xml..."

$preset2Path = Join-Path $presetsDir "Portrait.xml"
$preset2 = @'
<?xml version="1.0" encoding="UTF-8"?>
<Preset name="Portrait">
  <Exposure>0.3</Exposure>
  <Contrast>5</Contrast>
  <Shadows>10</Shadows>
  <Highlights>-10</Highlights>
  <WhiteBalance>AsShot</WhiteBalance>
</Preset>
'@
$preset2 | Set-Content -Path $preset2Path -Encoding UTF8
Write-Pass "Portrait preset written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath, $presetPath, $preset2Path)
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
Write-Host " FastRawViewer Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
