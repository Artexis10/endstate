# Copyright 2025 Substrate Systems OU
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Bumps the Endstate version following semver rules.

.DESCRIPTION
    Reads VERSION and SCHEMA_VERSION from the repo root, computes the new
    version, updates files, prepends a CHANGELOG entry, and creates a git
    commit + tag.

.PARAMETER Bump
    One of: patch, minor, major, schema-minor, schema-major.

.PARAMETER SetVersion
    Explicitly set the CLI version string (e.g. "1.2.3").

.PARAMETER DryRun
    Print what would happen without writing any files or creating commits.
#>
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet("patch", "minor", "major", "schema-minor", "schema-major")]
    [string]$Bump,

    [Parameter(Mandatory = $false)]
    [string]$SetVersion,

    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Resolve repo root as the parent of the script's own directory
$RepoRoot = Split-Path -Parent $PSScriptRoot

# --- Read current versions ---
$versionFile = Join-Path $RepoRoot "VERSION"
$schemaFile  = Join-Path $RepoRoot "SCHEMA_VERSION"

if (-not (Test-Path $versionFile)) {
    Write-Error "VERSION file not found at $versionFile"
}
if (-not (Test-Path $schemaFile)) {
    Write-Error "SCHEMA_VERSION file not found at $schemaFile"
}

$currentVersion = (Get-Content -Path $versionFile -Raw).Trim()
$currentSchema  = (Get-Content -Path $schemaFile -Raw).Trim()

# Parse current CLI version
$parts = $currentVersion -split '\.'
$major = [int]$parts[0]
$minor = [int]$parts[1]
$patch = [int]$parts[2]

# Parse current schema version
$schemaParts = $currentSchema -split '\.'
$schemaMajor = [int]$schemaParts[0]
$schemaMinor = [int]$schemaParts[1]

# --- Compute new versions ---
$newMajor = $major
$newMinor = $minor
$newPatch = $patch
$newSchemaMajor = $schemaMajor
$newSchemaMinor = $schemaMinor
$schemaChanged = $false

if ($SetVersion) {
    if ($SetVersion -notmatch '^\d+\.\d+\.\d+$') {
        Write-Error "SetVersion must be in x.y.z format, got: $SetVersion"
    }
    $setParts = $SetVersion -split '\.'
    $newMajor = [int]$setParts[0]
    $newMinor = [int]$setParts[1]
    $newPatch = [int]$setParts[2]
}
elseif ($Bump) {
    switch ($Bump) {
        "patch" {
            $newPatch = $patch + 1
        }
        "minor" {
            $newMinor = $minor + 1
            $newPatch = 0
        }
        "major" {
            $newMajor = $major + 1
            $newMinor = 0
            $newPatch = 0
        }
        "schema-minor" {
            $newSchemaMinor = $schemaMinor + 1
            $schemaChanged = $true
        }
        "schema-major" {
            $newSchemaMajor = $schemaMajor + 1
            $newSchemaMinor = 0
            $schemaChanged = $true
            # Schema major bump forces CLI major bump
            $newMajor = $major + 1
            $newMinor = 0
            $newPatch = 0
        }
    }
}
else {
    Write-Error "Must specify either -Bump or -SetVersion"
}

$newVersion = "$newMajor.$newMinor.$newPatch"
$newSchema  = "$newSchemaMajor.$newSchemaMinor"
$cliChanged = $newVersion -ne $currentVersion
$today = Get-Date -Format "yyyy-MM-dd"

# Determine the label for changelog/commit/output
$changeLabel = if ($cliChanged) { $newVersion } else { "schema-$newSchema" }

# --- Output plan ---
Write-Host "Current CLI version   : $currentVersion"
Write-Host "New CLI version       : $newVersion"
Write-Host "Current schema version: $currentSchema"
Write-Host "New schema version    : $newSchema"
if ($schemaChanged) {
    Write-Host "Schema version changed: YES"
}
if (-not $cliChanged) {
    Write-Host "CLI version changed  : NO"
}
Write-Host "Date                  : $today"

if ($DryRun) {
    Write-Host ""
    if ($cliChanged) {
        Write-Host "[DRY RUN] Would write VERSION       = $newVersion"
    }
    if ($schemaChanged) {
        Write-Host "[DRY RUN] Would write SCHEMA_VERSION = $newSchema"
    }
    Write-Host "[DRY RUN] Would prepend CHANGELOG.md section for [$changeLabel]"
    Write-Host "[DRY RUN] Would commit: chore: bump version to $changeLabel"
    if ($cliChanged) {
        Write-Host "[DRY RUN] Would tag   : v$newVersion"
    }
    if ($schemaChanged) {
        Write-Host "[DRY RUN] Would tag   : schema-v$newSchema"
    }
    return
}

# --- Write VERSION (only if changed) ---
if ($cliChanged) {
    Set-Content -Path $versionFile -Value $newVersion -NoNewline
}

# --- Write SCHEMA_VERSION (only if changed) ---
if ($schemaChanged) {
    Set-Content -Path $schemaFile -Value $newSchema -NoNewline
}

# --- Prepend CHANGELOG entry ---
$changelogFile = Join-Path $RepoRoot "CHANGELOG.md"
if (Test-Path $changelogFile) {
    $changelog = Get-Content -Path $changelogFile -Raw
    $newEntry = @"

## [$changeLabel] - $today

### Added

### Changed

### Fixed
"@
    # Insert after the first ## section header line (before the first existing version entry)
    # Find the position of the first "## [" in the file
    $insertPoint = $changelog.IndexOf("`n## [")
    if ($insertPoint -ge 0) {
        $changelog = $changelog.Substring(0, $insertPoint) + "`n" + $newEntry + $changelog.Substring($insertPoint)
    }
    else {
        # Append at the end if no existing version entry found
        $changelog = $changelog + "`n" + $newEntry
    }
    Set-Content -Path $changelogFile -Value $changelog -NoNewline
}

# --- Git commit and tag ---
$filesToStage = @($changelogFile)
if ($cliChanged) {
    $filesToStage += $versionFile
}
if ($schemaChanged) {
    $filesToStage += $schemaFile
}

foreach ($f in $filesToStage) {
    git add $f
}

git commit -m "chore: bump version to $changeLabel"

if ($cliChanged) {
    git tag "v$newVersion"
}
if ($schemaChanged) {
    git tag "schema-v$newSchema"
}

Write-Host ""
if ($cliChanged) {
    Write-Host "Version bumped to $newVersion" -ForegroundColor Green
}
if ($schemaChanged) {
    Write-Host "Schema version bumped to $newSchema" -ForegroundColor Green
}
