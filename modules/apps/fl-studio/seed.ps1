<#
.SYNOPSIS
    Seeds meaningful FL Studio configuration for curation testing.

.DESCRIPTION
    Sets up FL Studio configuration files with representative non-default values
    WITHOUT creating any license data. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Presets/ (sample channel and plugin presets)
    - Templates/ (sample project template descriptor)
    - Scores/ (sample score/MIDI pattern descriptor)
    - Mixer presets/ (sample mixer preset)

    DOES NOT configure:
    - Registration/license files
    - Plugin DLLs
    - Audio samples

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
Write-Host " FL Studio Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$flDir = Join-Path $env:USERPROFILE "Documents\Image-Line\FL Studio"
$presetsDir = Join-Path $flDir "Presets\Channel presets\Endstate"
$templatesDir = Join-Path $flDir "Templates"
$scoresDir = Join-Path $flDir "Scores\Endstate"
$mixerPresetsDir = Join-Path $flDir "Mixer presets\Endstate"

foreach ($dir in @($presetsDir, $templatesDir, $scoresDir, $mixerPresetsDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# CHANNEL PRESET
# ============================================================================
Write-Step "Writing sample channel preset..."

$presetPath = Join-Path $presetsDir "Endstate Pad.fnbsc"
$preset = @'
<?xml version="1.0" encoding="UTF-8"?>
<FLChannelPreset version="1" name="Endstate Pad">
    <Generator>3x Osc</Generator>
    <Settings>
        <Osc1 shape="sine" coarse="0" fine="0" />
        <Osc2 shape="triangle" coarse="12" fine="5" />
        <Osc3 shape="sine" coarse="-12" fine="-3" />
        <Mix osc1="80" osc2="60" osc3="40" />
        <Filter type="lowpass" cutoff="8000" resonance="20" />
        <Envelope attack="500" decay="200" sustain="70" release="1000" />
    </Settings>
</FLChannelPreset>
'@
$preset | Set-Content -Path $presetPath -Encoding UTF8
Write-Pass "Channel preset written to: $presetPath"

# ============================================================================
# TEMPLATE DESCRIPTOR
# ============================================================================
Write-Step "Writing sample template descriptor..."

$templatePath = Join-Path $templatesDir "Endstate Basic.txt"
$template = @"
FL Studio Template: Endstate Basic
BPM: 140
Time Signature: 4/4
Mixer Tracks:
  - Master (Insert 0)
  - Kick (Insert 1) - Fruity Limiter
  - Snare (Insert 2) - Fruity Parametric EQ 2
  - Hi-Hat (Insert 3) - Fruity Parametric EQ 2
  - Bass (Insert 4) - Fruity Soft Clipper
  - Lead (Insert 5) - Fruity Reverb 2, Fruity Delay 3
  - Pad (Insert 6) - Fruity Reverb 2
  - FX Send (Insert 10) - Fruity Reverb 2
Notes: Basic production template with common routing
"@
$template | Set-Content -Path $templatePath -Encoding UTF8
Write-Pass "Template descriptor written to: $templatePath"

# ============================================================================
# SCORE PATTERN
# ============================================================================
Write-Step "Writing sample score..."

$scorePath = Join-Path $scoresDir "Endstate Chord Progression.txt"
$score = @"
FL Studio Score: Endstate Chord Progression
Type: MIDI Pattern
Key: C Minor
BPM: 140
Length: 4 bars
Pattern:
  Bar 1: Cm (C3-Eb3-G3)
  Bar 2: Ab (Ab2-C3-Eb3)
  Bar 3: Bb (Bb2-D3-F3)
  Bar 4: G  (G2-B2-D3)
Notes: Common minor chord progression for electronic music
"@
$score | Set-Content -Path $scorePath -Encoding UTF8
Write-Pass "Score written to: $scorePath"

# ============================================================================
# MIXER PRESET
# ============================================================================
Write-Step "Writing sample mixer preset..."

$mixerPath = Join-Path $mixerPresetsDir "Endstate Vocal Chain.fnbsc"
$mixerPreset = @'
<?xml version="1.0" encoding="UTF-8"?>
<FLMixerPreset version="1" name="Endstate Vocal Chain">
    <Slots>
        <Slot index="0">
            <Plugin>Fruity Parametric EQ 2</Plugin>
            <Settings>
                <Band index="1" freq="80" gain="-inf" type="highpass" />
                <Band index="3" freq="3000" gain="2.0" type="peak" q="1.5" />
                <Band index="5" freq="12000" gain="1.5" type="highshelf" />
            </Settings>
        </Slot>
        <Slot index="1">
            <Plugin>Fruity Compressor</Plugin>
            <Settings>
                <Threshold>-18</Threshold>
                <Ratio>4</Ratio>
                <Attack>10</Attack>
                <Release>100</Release>
                <Gain>4</Gain>
            </Settings>
        </Slot>
        <Slot index="2">
            <Plugin>Fruity Reverb 2</Plugin>
            <Settings>
                <Mix>15</Mix>
                <Size>50</Size>
                <Decay>2000</Decay>
            </Settings>
        </Slot>
    </Slots>
</FLMixerPreset>
'@
$mixerPreset | Set-Content -Path $mixerPath -Encoding UTF8
Write-Pass "Mixer preset written to: $mixerPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($presetPath, $templatePath, $scorePath, $mixerPath)
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
Write-Host " FL Studio Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
