<#
.SYNOPSIS
    Curation runner stub for VSCodium.

.DESCRIPTION
    Placeholder for VSCodium curation workflow. Currently not implemented.
    
    When implemented, this script will:
    1. Ensure VSCodium is installed (winget: VSCodium.VSCodium)
    2. Seed meaningful configuration (settings.json, keybindings, extensions)
    3. Run capture/discovery diff
    4. Emit draft module and curation report

.PARAMETER Mode
    Execution mode: 'sandbox' (default) or 'local'.

.PARAMETER SkipInstall
    Skip VSCodium installation (assumes already installed).

.PARAMETER Promote
    Promote curated module to modules/apps/vscodium/.

.PARAMETER WriteModule
    Alias for -Promote.

.EXAMPLE
    .\curate-vscodium.ps1 -Mode local -SkipInstall
    # Currently throws "not implemented"
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet('sandbox', 'local')]
    [string]$Mode = 'sandbox',
    
    [Parameter(Mandatory = $false)]
    [switch]$SkipInstall,
    
    [Parameter(Mandatory = $false)]
    [switch]$Promote,
    
    [Parameter(Mandatory = $false)]
    [switch]$WriteModule
)

$ErrorActionPreference = 'Stop'

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Yellow
Write-Host " VSCodium Curation - NOT IMPLEMENTED" -ForegroundColor Yellow
Write-Host "=" * 60 -ForegroundColor Yellow
Write-Host ""

Write-Host "This curation runner is a stub. To implement:" -ForegroundColor Gray
Write-Host ""
Write-Host "  1. Identify VSCodium config locations:" -ForegroundColor White
Write-Host "     - %APPDATA%\VSCodium\User\settings.json" -ForegroundColor Gray
Write-Host "     - %APPDATA%\VSCodium\User\keybindings.json" -ForegroundColor Gray
Write-Host "     - %USERPROFILE%\.vscode-oss\extensions\*" -ForegroundColor Gray
Write-Host ""
Write-Host "  2. Create seed-vscodium-config.ps1 to populate config" -ForegroundColor White
Write-Host ""
Write-Host "  3. Implement Invoke-LocalCuration and Invoke-SandboxCuration" -ForegroundColor White
Write-Host "     following the pattern in curate-git.ps1" -ForegroundColor Gray
Write-Host ""
Write-Host "  4. Winget ID: VSCodium.VSCodium" -ForegroundColor White
Write-Host ""

throw "VSCodium curation not yet implemented. See curate-git.ps1 for reference implementation."
