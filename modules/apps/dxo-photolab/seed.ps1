<#
.SYNOPSIS
    Seeds meaningful DxO PhotoLab configuration for curation testing.

.DESCRIPTION
    Sets up DxO PhotoLab configuration files with representative non-default values
    WITHOUT creating any license data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Presets/ (sample processing preset)
    - Workspaces/ (sample workspace layout)

    DOES NOT configure:
    - License files
    - Database files (.db)
    - Optics Modules

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
Write-Host " DxO PhotoLab Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS (use representative version folder)
# ============================================================================
$dxoDir = Join-Path $env:LOCALAPPDATA "DxO\DxO PhotoLab 7"
$presetsDir = Join-Path $dxoDir "Presets"
$workspacesDir = Join-Path $dxoDir "Workspaces"

foreach ($dir in @($presetsDir, $workspacesDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# SAMPLE PRESET
# ============================================================================
Write-Step "Writing sample preset..."

$presetPath = Join-Path $presetsDir "Endstate Natural.preset"
$preset = @'
<?xml version="1.0" encoding="UTF-8"?>
<DxOPreset version="14" name="Endstate Natural">
    <Description>Clean natural processing with subtle enhancements</Description>
    <Settings>
        <ExposureCompensation>0.30</ExposureCompensation>
        <Contrast>10</Contrast>
        <Microcontrast>15</Microcontrast>
        <Vibrancy>10</Vibrancy>
        <Saturation>-5</Saturation>
        <HighlightsRecovery>20</HighlightsRecovery>
        <ShadowsRecovery>15</ShadowsRecovery>
        <ClearView>10</ClearView>
        <NoiseReduction>
            <Luminance>40</Luminance>
            <Chrominance>100</Chrominance>
            <DeadPixels>auto</DeadPixels>
        </NoiseReduction>
        <Sharpening>
            <Intensity>50</Intensity>
            <Radius>0.5</Radius>
        </Sharpening>
        <LensCorrection>
            <Distortion>auto</Distortion>
            <Vignetting>auto</Vignetting>
            <ChromaticAberration>auto</ChromaticAberration>
        </LensCorrection>
    </Settings>
</DxOPreset>
'@
$preset | Set-Content -Path $presetPath -Encoding UTF8
Write-Pass "Preset written to: $presetPath"

# ============================================================================
# SAMPLE WORKSPACE
# ============================================================================
Write-Step "Writing sample workspace..."

$workspacePath = Join-Path $workspacesDir "Endstate Editing.workspace"
$workspace = @'
<?xml version="1.0" encoding="UTF-8"?>
<DxOWorkspace version="1" name="Endstate Editing">
    <Panels>
        <Panel name="ImageBrowser" position="bottom" height="180" visible="true" />
        <Panel name="Histogram" position="topright" visible="true" />
        <Panel name="LightPalette" position="right" visible="true" />
        <Panel name="ColorPalette" position="right" visible="true" />
        <Panel name="DetailPalette" position="right" visible="true" />
    </Panels>
    <Viewer>
        <BackgroundColor>#1e1e1e</BackgroundColor>
        <ShowGrid>false</ShowGrid>
    </Viewer>
</DxOWorkspace>
'@
$workspace | Set-Content -Path $workspacePath -Encoding UTF8
Write-Pass "Workspace written to: $workspacePath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($presetPath, $workspacePath)
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
Write-Host " DxO PhotoLab Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
