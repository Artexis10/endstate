$ErrorActionPreference = "Stop"

$Path = Join-Path $PSScriptRoot "..\\.windsurf\\rules\\project-ruleset.md"

if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
    throw "Expected leaf file not found: $Path"
}

$content = Get-Content -LiteralPath $Path -Raw -Encoding UTF8

$bundleSection = @"


## Bundle Convention (Configuration Capture + Restore)

A **bundle** is a local folder containing configuration files and a manifest snapshot.

**Default location**: `<manifestDir>/bundle/`  
**Custom location**: Use `--bundle <path>` flag

**Core Rules**:
- Manifest is single source of truth (engine always uses explicitly provided manifest)
- Never auto-load snapshot (manifest.snapshot.jsonc is for audit/comparison only)
- Warn on snapshot mismatch (does not fail)
- Relative paths preserved in bundle structure
- Local folder only (no remote/encryption/zip in MVP)

**Commands**:
- `capture-config`: Copies system configs to bundle (inverse of restore)
- `validate-bundle`: Validates bundle integrity before restore
- `restore`: Applies bundle configs to system (requires --enable-restore)

**Safety**: No auto-secret capture, sensitive path warnings, backup-first restore.

"@

$insertPoint = $content.IndexOf("## CLI Commands")
if ($insertPoint -eq -1) {
    throw "Could not find '## CLI Commands' section"
}

$content = $content.Insert($insertPoint, $bundleSection)

Set-Content -LiteralPath $Path -Value $content -Encoding UTF8 -NoNewline

Write-Host "Ruleset updated successfully with bundle convention"
