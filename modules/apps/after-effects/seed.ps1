<#
.SYNOPSIS
    Seeds meaningful Adobe After Effects configuration for curation testing.

.DESCRIPTION
    Sets up After Effects configuration files with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - Presets/ (effect presets)
    - Workspaces/ (workspace layouts)
    - aeks/ (keyboard shortcuts)

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
Write-Host " After Effects Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS (use a representative version folder)
# ============================================================================
$aeDir = Join-Path $env:APPDATA "Adobe\After Effects\25.0"
$presetsDir = Join-Path $aeDir "Presets"
$workspacesDir = Join-Path $aeDir "Workspaces"
$aeksDir = Join-Path $aeDir "aeks"

foreach ($dir in @($presetsDir, $workspacesDir, $aeksDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# PRESETS
# ============================================================================
Write-Step "Writing Presets/Endstate-FadeIn.ffx..."

$presetPath = Join-Path $presetsDir "Endstate-FadeIn.ffx"
$preset = @'
<?xml version="1.0" encoding="UTF-8"?>
<AfterEffectsPreset version="1" name="Endstate-FadeIn">
  <Effect name="Transform">
    <Opacity>
      <Keyframe time="0" value="0" />
      <Keyframe time="30" value="100" />
    </Opacity>
  </Effect>
</AfterEffectsPreset>
'@
$preset | Set-Content -Path $presetPath -Encoding UTF8
Write-Pass "FadeIn preset written"

Write-Step "Writing Presets/Endstate-ScaleUp.ffx..."

$preset2Path = Join-Path $presetsDir "Endstate-ScaleUp.ffx"
$preset2 = @'
<?xml version="1.0" encoding="UTF-8"?>
<AfterEffectsPreset version="1" name="Endstate-ScaleUp">
  <Effect name="Transform">
    <Scale>
      <Keyframe time="0" value="80" />
      <Keyframe time="20" value="100" />
    </Scale>
  </Effect>
</AfterEffectsPreset>
'@
$preset2 | Set-Content -Path $preset2Path -Encoding UTF8
Write-Pass "ScaleUp preset written"

# ============================================================================
# WORKSPACE
# ============================================================================
Write-Step "Writing Workspaces/Endstate-Compositing.xml..."

$workspacePath = Join-Path $workspacesDir "Endstate-Compositing.xml"
$workspace = @'
<?xml version="1.0" encoding="UTF-8"?>
<Workspace name="Endstate-Compositing" version="1">
  <Panel name="Composition" position="center" width="60%" />
  <Panel name="Timeline" position="bottom" height="35%" />
  <Panel name="Project" position="left" width="25%" />
  <Panel name="Effects" position="right" width="20%" />
  <Panel name="Character" position="right-bottom" height="30%" />
</Workspace>
'@
$workspace | Set-Content -Path $workspacePath -Encoding UTF8
Write-Pass "Workspace written"

# ============================================================================
# KEYBOARD SHORTCUTS
# ============================================================================
Write-Step "Writing aeks/Endstate.aeks..."

$aeksPath = Join-Path $aeksDir "Endstate.aeks"
$aeks = @'
<?xml version="1.0" encoding="UTF-8"?>
<KeyboardShortcuts version="1" name="Endstate">
  <Shortcut command="SplitLayer" key="Ctrl+Shift+D" />
  <Shortcut command="PreCompose" key="Ctrl+Shift+C" />
  <Shortcut command="SetInPoint" key="Alt+[" />
  <Shortcut command="SetOutPoint" key="Alt+]" />
  <Shortcut command="RAMPreview" key="Numpad0" />
  <Shortcut command="ToggleTransparencyGrid" key="Ctrl+Alt+G" />
</KeyboardShortcuts>
'@
$aeks | Set-Content -Path $aeksPath -Encoding UTF8
Write-Pass "Keyboard shortcuts written"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($presetPath, $preset2Path, $workspacePath, $aeksPath)
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
Write-Host " After Effects Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
