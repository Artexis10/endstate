<#
.SYNOPSIS
    Seeds meaningful Affinity Photo 2 configuration for curation testing.

.DESCRIPTION
    Sets up Affinity Photo 2 configuration files with representative non-default values
    WITHOUT creating any credentials. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - user/shortcuts.json (keyboard shortcuts)
    - user/palettes/ (sample palette)
    - user/macros/ (sample macro)

    DOES NOT configure:
    - License/activation data
    - Recovery files

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
Write-Host " Affinity Photo 2 Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$affinityDir = Join-Path $env:APPDATA "Affinity\Photo 2\user"
$palettesDir = Join-Path $affinityDir "palettes"
$macrosDir = Join-Path $affinityDir "macros"

foreach ($dir in @($affinityDir, $palettesDir, $macrosDir)) {
    if (-not (Test-Path $dir)) {
        Write-Step "Creating directory: $dir"
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# ============================================================================
# KEYBOARD SHORTCUTS
# ============================================================================
Write-Step "Writing shortcuts.json..."

$shortcutsPath = Join-Path $affinityDir "shortcuts.json"
$shortcuts = @{
    version = 2
    name = "Endstate"
    shortcuts = @{
        "File.New" = "Ctrl+N"
        "File.Open" = "Ctrl+O"
        "File.Save" = "Ctrl+S"
        "File.SaveAs" = "Ctrl+Shift+S"
        "File.Export" = "Ctrl+Shift+E"
        "Edit.Undo" = "Ctrl+Z"
        "Edit.Redo" = "Ctrl+Shift+Z"
        "Edit.Copy" = "Ctrl+C"
        "Edit.Paste" = "Ctrl+V"
        "Edit.SelectAll" = "Ctrl+A"
        "Edit.Deselect" = "Ctrl+D"
        "View.ZoomIn" = "Ctrl+="
        "View.ZoomOut" = "Ctrl+-"
        "View.FitToWindow" = "Ctrl+0"
        "View.ActualSize" = "Ctrl+1"
        "Layer.NewLayer" = "Ctrl+Shift+N"
        "Layer.DuplicateLayer" = "Ctrl+J"
        "Layer.MergeDown" = "Ctrl+E"
        "Filter.GaussianBlur" = "Ctrl+Shift+G"
        "Adjustments.Levels" = "Ctrl+L"
        "Adjustments.Curves" = "Ctrl+M"
        "Adjustments.HSL" = "Ctrl+U"
    }
}
$shortcuts | ConvertTo-Json -Depth 5 | Set-Content -Path $shortcutsPath -Encoding UTF8
Write-Pass "Shortcuts written to: $shortcutsPath"

# ============================================================================
# SAMPLE PALETTE
# ============================================================================
Write-Step "Writing sample palette..."

$palettePath = Join-Path $palettesDir "Endstate Muted.json"
$palette = @{
    name = "Endstate Muted"
    type = "application"
    colors = @(
        @{ name = "Warm Shadow"; hex = "#2D2926" }
        @{ name = "Cool Gray"; hex = "#8B8C89" }
        @{ name = "Soft Cream"; hex = "#F5F0EB" }
        @{ name = "Dusty Rose"; hex = "#C4A882" }
        @{ name = "Sage Green"; hex = "#87A878" }
        @{ name = "Slate Blue"; hex = "#6B7F99" }
        @{ name = "Warm White"; hex = "#FAF8F5" }
        @{ name = "Deep Teal"; hex = "#2A5F5F" }
    )
}
$palette | ConvertTo-Json -Depth 5 | Set-Content -Path $palettePath -Encoding UTF8
Write-Pass "Palette written to: $palettePath"

# ============================================================================
# SAMPLE MACRO
# ============================================================================
Write-Step "Writing sample macro..."

$macroPath = Join-Path $macrosDir "Endstate Web Export.json"
$macro = @{
    name = "Endstate Web Export Prep"
    description = "Resize to 2048px, sharpen, convert to sRGB"
    steps = @(
        @{ action = "ResizeDocument"; params = @{ width = 2048; height = 2048; resampleMethod = "Lanczos3"; scaleMode = "FitInside" } }
        @{ action = "UnsharpMask"; params = @{ radius = 0.5; factor = 80; threshold = 1 } }
        @{ action = "ConvertColorProfile"; params = @{ profile = "sRGB IEC61966-2.1" } }
    )
}
$macro | ConvertTo-Json -Depth 5 | Set-Content -Path $macroPath -Encoding UTF8
Write-Pass "Macro written to: $macroPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($shortcutsPath, $palettePath, $macroPath)
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
Write-Host " Affinity Photo 2 Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
foreach ($f in $seededFiles) { Write-Host "  - $f" -ForegroundColor Gray }
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
