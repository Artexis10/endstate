<#
.SYNOPSIS
    Seeds meaningful Windsurf user configuration for curation testing.

.DESCRIPTION
    Sets up Windsurf configuration values that represent real-world user preferences
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - User settings (settings.json)
    - Keybindings (keybindings.json)

    DOES NOT configure:
    - Account sync tokens
    - API keys
    - Extension-specific secrets

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
Write-Host " Windsurf Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$windsurfUserDir = Join-Path $env:APPDATA "Windsurf\User"
$settingsPath = Join-Path $windsurfUserDir "settings.json"
$keybindingsPath = Join-Path $windsurfUserDir "keybindings.json"

# Ensure directory exists
if (-not (Test-Path $windsurfUserDir)) {
    Write-Step "Creating Windsurf user directory..."
    New-Item -ItemType Directory -Path $windsurfUserDir -Force | Out-Null
}

# ============================================================================
# SETTINGS.JSON
# ============================================================================
Write-Step "Writing settings.json..."

$settings = @{
    # Editor settings
    "editor.fontSize" = 14
    "editor.fontFamily" = "'Cascadia Code', 'Fira Code', Consolas, 'Courier New', monospace"
    "editor.fontLigatures" = $true
    "editor.tabSize" = 2
    "editor.insertSpaces" = $true
    "editor.wordWrap" = "off"
    "editor.minimap.enabled" = $true
    "editor.renderWhitespace" = "selection"
    "editor.rulers" = @(80, 120)
    "editor.formatOnSave" = $true
    "editor.bracketPairColorization.enabled" = $true
    "editor.guides.bracketPairs" = "active"
    "editor.stickyScroll.enabled" = $true
    "editor.smoothScrolling" = $true
    
    # Workbench settings
    "workbench.colorTheme" = "Default Dark+"
    "workbench.iconTheme" = "vs-seti"
    "workbench.startupEditor" = "welcomePage"
    "workbench.editor.enablePreview" = $true
    "workbench.tree.indent" = 16
    
    # Files settings
    "files.autoSave" = "afterDelay"
    "files.autoSaveDelay" = 1000
    "files.trimTrailingWhitespace" = $true
    "files.insertFinalNewline" = $true
    "files.encoding" = "utf8"
    "files.exclude" = @{
        "**/.git" = $true
        "**/.DS_Store" = $true
        "**/node_modules" = $true
    }
    
    # Terminal settings
    "terminal.integrated.fontSize" = 13
    "terminal.integrated.defaultProfile.windows" = "PowerShell"
    
    # Telemetry
    "telemetry.telemetryLevel" = "off"
}

$settingsJson = $settings | ConvertTo-Json -Depth 5
$settingsJson | Set-Content -Path $settingsPath -Encoding UTF8

Write-Pass "Settings written to: $settingsPath"

# ============================================================================
# KEYBINDINGS.JSON
# ============================================================================
Write-Step "Writing keybindings.json..."

$keybindings = @(
    @{
        key = "ctrl+shift+d"
        command = "editor.action.copyLinesDownAction"
    }
    @{
        key = "ctrl+shift+k"
        command = "editor.action.deleteLines"
    }
    @{
        key = "ctrl+/"
        command = "editor.action.commentLine"
        when = "editorTextFocus && !editorReadonly"
    }
    @{
        key = 'ctrl+`'
        command = "workbench.action.terminal.toggleTerminal"
    }
)

$keybindingsJson = $keybindings | ConvertTo-Json -Depth 5
$keybindingsJson | Set-Content -Path $keybindingsPath -Encoding UTF8

Write-Pass "Keybindings written to: $keybindingsPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath, $keybindingsPath)
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
Write-Host " Windsurf Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $settingsPath" -ForegroundColor Gray
Write-Host "  - $keybindingsPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
