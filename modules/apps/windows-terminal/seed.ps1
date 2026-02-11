<#
.SYNOPSIS
    Seeds meaningful Windows Terminal configuration for curation testing.

.DESCRIPTION
    Sets up Windows Terminal settings.json with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - settings.json (profiles, color schemes, keybindings)

    DOES NOT configure:
    - SSH keys or credentials
    - Environment-specific paths

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
Write-Host " Windows Terminal Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$wtLocalState = Join-Path $env:LOCALAPPDATA "Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState"
$settingsPath = Join-Path $wtLocalState "settings.json"

# Ensure directory exists
if (-not (Test-Path $wtLocalState)) {
    Write-Step "Creating Windows Terminal LocalState directory..."
    New-Item -ItemType Directory -Path $wtLocalState -Force | Out-Null
}

# ============================================================================
# SETTINGS.JSON
# ============================================================================
Write-Step "Writing settings.json..."

$settings = @{
    '$help' = "https://aka.ms/terminal-documentation"
    '$schema' = "https://aka.ms/terminal-profiles-schema"
    
    defaultProfile = "{574e775e-4f2a-5b96-ac1e-a2962a402336}"
    
    copyOnSelect = $false
    copyFormatting = $false
    
    profiles = @{
        defaults = @{
            font = @{
                face = "Cascadia Code"
                size = 11
            }
            padding = "8, 8, 8, 8"
            antialiasingMode = "grayscale"
            cursorShape = "bar"
            colorScheme = "One Half Dark"
        }
        list = @(
            @{
                guid = "{574e775e-4f2a-5b96-ac1e-a2962a402336}"
                name = "PowerShell"
                source = "Windows.Terminal.PowershellCore"
                hidden = $false
            }
            @{
                guid = "{0caa0dad-35be-5f56-a8ff-afceeeaa6101}"
                name = "Command Prompt"
                hidden = $false
            }
            @{
                guid = "{2c4de342-38b7-51cf-b940-2309a097f518}"
                name = "Ubuntu"
                source = "Windows.Terminal.Wsl"
                hidden = $false
            }
        )
    }
    
    schemes = @(
        @{
            name = "Endstate Dark"
            background = "#1E1E2E"
            foreground = "#CDD6F4"
            cursorColor = "#F5E0DC"
            selectionBackground = "#585B70"
            black = "#45475A"
            red = "#F38BA8"
            green = "#A6E3A1"
            yellow = "#F9E2AF"
            blue = "#89B4FA"
            purple = "#F5C2E7"
            cyan = "#94E2D5"
            white = "#BAC2DE"
            brightBlack = "#585B70"
            brightRed = "#F38BA8"
            brightGreen = "#A6E3A1"
            brightYellow = "#F9E2AF"
            brightBlue = "#89B4FA"
            brightPurple = "#F5C2E7"
            brightCyan = "#94E2D5"
            brightWhite = "#A6ADC8"
        }
    )
    
    actions = @(
        @{ command = @{ action = "copy"; singleLine = $false }; keys = "ctrl+c" }
        @{ command = "paste"; keys = "ctrl+v" }
        @{ command = "find"; keys = "ctrl+shift+f" }
        @{ command = @{ action = "splitPane"; split = "auto"; splitMode = "duplicate" }; keys = "alt+shift+d" }
        @{ command = "toggleFocusMode"; keys = "ctrl+shift+enter" }
    )
    
    theme = "dark"
    confirmCloseAllTabs = $true
    startOnUserLogin = $false
    initialCols = 120
    initialRows = 30
}

$settingsJson = $settings | ConvertTo-Json -Depth 10
$settingsJson | Set-Content -Path $settingsPath -Encoding UTF8

Write-Pass "Settings written to: $settingsPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($settingsPath)
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
Write-Host " Windows Terminal Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $settingsPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
