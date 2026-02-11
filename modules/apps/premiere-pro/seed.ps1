<#
.SYNOPSIS
    Seeds meaningful Adobe Premiere Pro configuration for curation testing.

.DESCRIPTION
    Sets up Premiere Pro configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Layouts/ (workspace layouts)
    - Win/ (keyboard shortcuts)
    - Effect Presets and Custom Items.prfpset (effect presets)

    DOES NOT configure:
    - Adobe account credentials
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
Write-Host " Premiere Pro Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS (use a representative version folder)
# ============================================================================
$ppDir = Join-Path $env:APPDATA "Adobe\Premiere Pro\25.0\Profile-HugoK\\"
$layoutsDir = Join-Path $ppDir "Layouts"
$winDir = Join-Path $ppDir "Win"

foreach ($dir in @($ppDir, $layoutsDir, $winDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# EFFECT PRESETS
# ============================================================================
Write-Step "Writing Effect Presets and Custom Items.prfpset..."

$presetsPath = Join-Path $ppDir "Effect Presets and Custom Items.prfpset"
$presets = @'
<?xml version="1.0" encoding="UTF-8"?>
<PremiereData Version="3">
  <EffectPresets>
    <Preset name="Endstate Color Grade">
      <Effect name="Lumetri Color">
        <Temperature>5500</Temperature>
        <Tint>0</Tint>
        <Exposure>0.2</Exposure>
        <Contrast>15</Contrast>
        <Highlights>-10</Highlights>
        <Shadows>15</Shadows>
        <Saturation>110</Saturation>
      </Effect>
    </Preset>
  </EffectPresets>
</PremiereData>
'@
$presets | Set-Content -Path $presetsPath -Encoding UTF8
Write-Pass "Effect presets written"

# ============================================================================
# WORKSPACE LAYOUT
# ============================================================================
Write-Step "Writing Layouts/Endstate-Editing.xml..."

$layoutPath = Join-Path $layoutsDir "Endstate-Editing.xml"
$layout = @'
<?xml version="1.0" encoding="UTF-8"?>
<Workspace name="Endstate-Editing" version="1">
  <Panel name="Timeline" position="bottom" height="40%" />
  <Panel name="Program" position="center" width="60%" />
  <Panel name="Source" position="center-left" width="40%" />
  <Panel name="Project" position="left" width="25%" />
  <Panel name="Effects" position="right" width="20%" />
  <Panel name="Audio Meters" position="right-bottom" height="30%" />
</Workspace>
'@
$layout | Set-Content -Path $layoutPath -Encoding UTF8
Write-Pass "Workspace layout written"

# ============================================================================
# KEYBOARD SHORTCUTS
# ============================================================================
Write-Step "Writing Win/Endstate.kys..."

$kysPath = Join-Path $winDir "Endstate.kys"
$kys = @'
<?xml version="1.0" encoding="UTF-8"?>
<KeyboardShortcuts version="1" name="Endstate">
  <Shortcut command="RippleDelete" key="Shift+Delete" />
  <Shortcut command="RazorTool" key="C" />
  <Shortcut command="AddEdit" key="Ctrl+K" />
  <Shortcut command="MarkIn" key="I" />
  <Shortcut command="MarkOut" key="O" />
  <Shortcut command="InsertClip" key="," />
  <Shortcut command="OverwriteClip" key="." />
  <Shortcut command="Undo" key="Ctrl+Z" />
  <Shortcut command="Redo" key="Ctrl+Shift+Z" />
</KeyboardShortcuts>
'@
$kys | Set-Content -Path $kysPath -Encoding UTF8
Write-Pass "Keyboard shortcuts written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($presetsPath, $layoutPath, $kysPath)
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
Write-Host " Premiere Pro Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
