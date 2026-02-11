<#
.SYNOPSIS
    Seeds meaningful Obsidian app-level configuration for curation testing.

.DESCRIPTION
    Sets up Obsidian app-level configuration with representative non-default values
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - obsidian.json (app-level settings: vault list, theme, language)

    DOES NOT configure:
    - Vault-specific settings (.obsidian/ inside vaults)
    - Sync tokens or authentication data
    - Actual vault content

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
Write-Host " Obsidian Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# ============================================================================
# PATHS
# ============================================================================
$obsidianDir = Join-Path $env:APPDATA "obsidian"
$obsidianJsonPath = Join-Path $obsidianDir "obsidian.json"

# Ensure directory exists
if (-not (Test-Path $obsidianDir)) {
    Write-Step "Creating Obsidian config directory..."
    New-Item -ItemType Directory -Path $obsidianDir -Force | Out-Null
}

# ============================================================================
# OBSIDIAN.JSON
# ============================================================================
Write-Step "Writing obsidian.json..."

$obsidianConfig = @{
    vaults = @{
        "a1b2c3d4e5f6a1b2" = @{
            path = "C:\\Users\\placeholder\\Documents\\Notes"
            ts = 1700000000000
            open = $true
        }
        "f6e5d4c3b2a1f6e5" = @{
            path = "C:\\Users\\placeholder\\Documents\\Work"
            ts = 1700000001000
            open = $false
        }
    }
    updateDisabled = $false
    frameless = $true
    theme = "obsidian"
    language = "en"
}

$obsidianJson = $obsidianConfig | ConvertTo-Json -Depth 5
$obsidianJson | Set-Content -Path $obsidianJsonPath -Encoding UTF8

Write-Pass "Config written to: $obsidianJsonPath"

# ============================================================================
# POST-SEED DIAGNOSTIC
# ============================================================================
Write-Host ""
Write-Step "Post-seed diagnostic:"
$seededFiles = @($obsidianJsonPath)
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
Write-Host " Obsidian Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Files created:"
Write-Host "  - $obsidianJsonPath" -ForegroundColor Gray
Write-Host ""

Write-Pass "Seeding complete"
Write-Host ""
exit 0
