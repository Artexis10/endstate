<#
.SYNOPSIS
    SCHEMA_VERSION format validation for the pre-push hook.

.DESCRIPTION
    Validates that SCHEMA_VERSION at the repo root is a well-formed MAJOR.MINOR
    string. Invoked via `pwsh -NoProfile -File` (not an inline -Command) so that
    the POSIX shell lefthook uses on Linux/WSL cannot expand the script's
    variables before pwsh runs — the inline form was a silent no-op there.

    The engine CLI version is owned by release-please (.release-please-manifest.json
    + the git tag via ldflags); there is no VERSION file to validate.

    Exit code 0 = valid, non-zero = invalid (blocks push).
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$schemaPath = Join-Path $PSScriptRoot '..' 'SCHEMA_VERSION'
if (-not (Test-Path $schemaPath)) {
    Write-Host "SCHEMA_VERSION not found at repo root." -ForegroundColor Red
    exit 1
}

$schema = (Get-Content $schemaPath -Raw).Trim()
if ($schema -notmatch '^\d+\.\d+$') {
    Write-Host "SCHEMA_VERSION format invalid: '$schema' (expected MAJOR.MINOR, e.g. 1.0)." -ForegroundColor Red
    exit 1
}

Write-Host "SCHEMA_VERSION=$schema OK" -ForegroundColor Green
exit 0
