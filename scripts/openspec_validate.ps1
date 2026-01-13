<#
.SYNOPSIS
    OpenSpec validation wrapper for pre-push hook.

.DESCRIPTION
    Runs OpenSpec validation via npm script. Supports OPENSPEC_BYPASS=1 for emergencies.
    Exit code 0 = success, non-zero = failure (blocks push).
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# Check for bypass
if ($env:OPENSPEC_BYPASS -eq '1') {
    Write-Warning "OPENSPEC_BYPASS=1 detected. Skipping OpenSpec validation."
    Write-Warning "This should only be used for emergencies. Document reason in commit."
    exit 0
}

# Run validation via npm script
Write-Host "Running OpenSpec validation..." -ForegroundColor Cyan

try {
    $result = & npm run openspec:validate 2>&1
    $exitCode = $LASTEXITCODE

    # Output the result
    $result | ForEach-Object { Write-Host $_ }

    if ($exitCode -ne 0) {
        Write-Host ""
        Write-Host "OpenSpec validation FAILED. Push blocked." -ForegroundColor Red
        Write-Host "Fix validation errors or set OPENSPEC_BYPASS=1 for emergency bypass." -ForegroundColor Yellow
        exit $exitCode
    }

    Write-Host "OpenSpec validation passed." -ForegroundColor Green
    exit 0
}
catch {
    Write-Host "OpenSpec validation error: $_" -ForegroundColor Red
    Write-Host "Push blocked. Set OPENSPEC_BYPASS=1 for emergency bypass." -ForegroundColor Yellow
    exit 1
}
