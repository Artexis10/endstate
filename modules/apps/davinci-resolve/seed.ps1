<#
.SYNOPSIS
    Seeds meaningful DaVinci Resolve configuration for curation testing.

.DESCRIPTION
    Sets up DaVinci Resolve configuration files with representative non-default values
    WITHOUT creating any credentials. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Presets/ (sample render preset)
    - LUT/ (sample 1D LUT)
    - PowerGrades/ (sample color grading preset)
    - Keyboard Layouts/ (sample keyboard layout)

    DOES NOT configure:
    - Project databases
    - Cache files
    - License/activation data

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
Write-Host " DaVinci Resolve Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$resolveDir = Join-Path $env:APPDATA "Blackmagic Design\DaVinci Resolve"
$presetsDir = Join-Path $resolveDir "Presets"
$lutDir = Join-Path $resolveDir "LUT"
$powerGradesDir = Join-Path $resolveDir "PowerGrades"
$keyboardDir = Join-Path $resolveDir "Keyboard Layouts"

foreach ($dir in @($presetsDir, $lutDir, $powerGradesDir, $keyboardDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# RENDER PRESET
# ============================================================================
Write-Step "Writing sample render preset..."

$renderPresetPath = Join-Path $presetsDir "Endstate YouTube 1080p.xml"
$renderPreset = @'
<?xml version="1.0" encoding="UTF-8"?>
<RenderPreset version="1" name="Endstate YouTube 1080p">
    <Format>mp4</Format>
    <Codec>H.264</Codec>
    <Resolution>
        <Width>1920</Width>
        <Height>1080</Height>
    </Resolution>
    <FrameRate>60</FrameRate>
    <Quality>
        <BitrateMode>CBR</BitrateMode>
        <Bitrate>20000</Bitrate>
    </Quality>
    <Audio>
        <Codec>AAC</Codec>
        <Bitrate>320</Bitrate>
        <SampleRate>48000</SampleRate>
    </Audio>
</RenderPreset>
'@
$renderPreset | Set-Content -Path $renderPresetPath -Encoding UTF8
Write-Pass "Render preset written to: $renderPresetPath"

# ============================================================================
# SAMPLE LUT (1D LUT placeholder)
# ============================================================================
Write-Step "Writing sample LUT..."

$lutPath = Join-Path $lutDir "Endstate_Warm.cube"
# Minimal valid .cube LUT (identity with slight warm shift)
$lut = @"
TITLE "Endstate Warm"
LUT_1D_SIZE 4
DOMAIN_MIN 0.0 0.0 0.0
DOMAIN_MAX 1.0 1.0 1.0
0.000000 0.000000 0.000000
0.340000 0.330000 0.310000
0.680000 0.660000 0.630000
1.000000 0.980000 0.950000
"@
$lut | Set-Content -Path $lutPath -Encoding UTF8
Write-Pass "LUT written to: $lutPath"

# ============================================================================
# POWERGRADE
# ============================================================================
Write-Step "Writing sample PowerGrade..."

$powerGradePath = Join-Path $powerGradesDir "Endstate Film Look.dpx"
$powerGrade = @'
<?xml version="1.0" encoding="UTF-8"?>
<PowerGrade version="1" name="Endstate Film Look">
    <Description>Cinematic film emulation with lifted blacks and rolled highlights</Description>
    <ColorWheels>
        <Lift>
            <Red>0.02</Red>
            <Green>0.01</Green>
            <Blue>0.03</Blue>
        </Lift>
        <Gamma>
            <Red>0.00</Red>
            <Green>-0.01</Green>
            <Blue>0.01</Blue>
        </Gamma>
        <Gain>
            <Red>1.02</Red>
            <Green>1.00</Green>
            <Blue>0.97</Blue>
        </Gain>
    </ColorWheels>
    <Contrast>1.10</Contrast>
    <Saturation>0.85</Saturation>
    <HighlightRolloff>0.90</HighlightRolloff>
</PowerGrade>
'@
$powerGrade | Set-Content -Path $powerGradePath -Encoding UTF8
Write-Pass "PowerGrade written to: $powerGradePath"

# ============================================================================
# KEYBOARD LAYOUT
# ============================================================================
Write-Step "Writing sample keyboard layout..."

$kbLayoutPath = Join-Path $keyboardDir "Endstate.txt"
$kbLayout = @'
# DaVinci Resolve Keyboard Layout - Endstate
# Format: ActionName = KeyCombination
PlayForward = Space
PlayReverse = J
Stop = K
StepForward = L
StepBackward = ;
MarkIn = I
MarkOut = O
Cut = Ctrl+B
Delete = Delete
Undo = Ctrl+Z
Redo = Ctrl+Shift+Z
FullScreen = Ctrl+F
RenderInOut = Ctrl+Shift+R
SwitchToEdit = Shift+3
SwitchToColor = Shift+6
SwitchToDeliver = Shift+8
'@
$kbLayout | Set-Content -Path $kbLayoutPath -Encoding UTF8
Write-Pass "Keyboard layout written to: $kbLayoutPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($renderPresetPath, $lutPath, $powerGradePath, $kbLayoutPath)
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
Write-Host " DaVinci Resolve Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
