<#
.SYNOPSIS
    Endstate CLI shim - thin wrapper that delegates to repo installation.

.DESCRIPTION
    This shim is installed to %LOCALAPPDATA%\Endstate\bin\endstate.ps1
    It resolves the repo root and delegates all commands to the real endstate.ps1 in the repo.
    
    Repo root resolution priority:
    1. $env:ENDSTATE_ROOT (if set and valid)
    2. %LOCALAPPDATA%\Endstate\repo-root.txt (if exists and valid)
    3. Error: repo root not configured

.NOTES
    This file is a template. Bootstrap will install it to %LOCALAPPDATA%\Endstate\bin\endstate.ps1
    
    This script accepts all arguments via $args and forwards them to the repo entrypoint.
#>

$ErrorActionPreference = "Stop"

function Get-RepoRootPath {
    <#
    .SYNOPSIS
        Resolve repo root path from environment or persisted file.
    #>
    
    # Priority 1: Environment variable override
    if ($env:ENDSTATE_ROOT) {
        if (Test-Path $env:ENDSTATE_ROOT) {
            return $env:ENDSTATE_ROOT
        } else {
            Write-Warning "ENDSTATE_ROOT is set but path does not exist: $env:ENDSTATE_ROOT"
        }
    }
    
    # Priority 2: Persisted repo-root.txt
    $repoRootFile = Join-Path $env:LOCALAPPDATA "Endstate\repo-root.txt"
    if (Test-Path $repoRootFile) {
        try {
            $persistedRoot = (Get-Content -Path $repoRootFile -Raw -ErrorAction Stop).Trim()
            if ($persistedRoot -and (Test-Path $persistedRoot)) {
                return $persistedRoot
            }
        } catch {
            Write-Warning "Failed to read repo-root.txt: $_"
        }
    }
    
    return $null
}

# Resolve repo root
$repoRoot = Get-RepoRootPath

if (-not $repoRoot) {
    Write-Host ""
    Write-Host "[ERROR] Endstate repo root not configured." -ForegroundColor Red
    Write-Host ""
    Write-Host "To configure, run one of the following:" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  Option 1: Set environment variable (session or persistent)" -ForegroundColor Cyan
    Write-Host "    `$env:ENDSTATE_ROOT = 'C:\path\to\endstate'" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Option 2: Run bootstrap from the repo directory" -ForegroundColor Cyan
    Write-Host "    cd C:\path\to\endstate" -ForegroundColor DarkGray
    Write-Host "    .\endstate.ps1 bootstrap -RepoRoot `$PWD" -ForegroundColor DarkGray
    Write-Host ""
    exit 1
}

# Verify repo structure
$repoEntrypoint = Join-Path $repoRoot "endstate.ps1"
if (-not (Test-Path $repoEntrypoint)) {
    Write-Host ""
    Write-Host "[ERROR] Repo entrypoint not found: $repoEntrypoint" -ForegroundColor Red
    Write-Host ""
    Write-Host "The configured repo root may be invalid:" -ForegroundColor Yellow
    Write-Host "  $repoRoot" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "To reconfigure, run bootstrap from the correct repo directory:" -ForegroundColor Yellow
    Write-Host "  cd C:\path\to\endstate" -ForegroundColor DarkGray
    Write-Host "  .\endstate.ps1 bootstrap -RepoRoot `$PWD" -ForegroundColor DarkGray
    Write-Host ""
    exit 1
}

# Delegate to repo entrypoint, forwarding all arguments
& $repoEntrypoint @args

# Preserve exit code
$exitCode = $LASTEXITCODE
if ($null -eq $exitCode) {
    $exitCode = 0
}

exit $exitCode
