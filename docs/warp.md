# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

Endstate is a declarative machine provisioning system written in PowerShell. It eliminates the "clean install tax" by enabling safe, repeatable, auditable machine rebuilds from manifests. The system follows strict principles: declarative desired state, idempotence, non-destructive defaults, and verification-first design.

## Essential Commands

### Testing
```powershell
# Run all tests
.\scripts\test_pester.ps1

# Run specific test suite
.\scripts\test_pester.ps1 -Path tests/unit

# Run specific test file
.\scripts\test_pester.ps1 -Path tests/unit/Manifest.Tests.ps1

# Run tests with tag filter
.\scripts\test_pester.ps1 -Tag "Manifest"
```

Tests use Pester 5.7.1 (vendored in `tools/pester/`). Test results are written to `test-results.xml`.

### CLI Commands
```powershell
# Capture current machine state
.\bin\cli.ps1 -Command capture -Profile my-machine

# Capture with templates for restore/verify
.\bin\cli.ps1 -Command capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate

# Update existing manifest with new capture
.\bin\cli.ps1 -Command capture -Profile my-machine -Update

# Generate execution plan
.\bin\cli.ps1 -Command plan -Manifest manifests\my-machine.jsonc

# Dry-run (preview changes)
.\bin\cli.ps1 -Command apply -Manifest manifests\my-machine.jsonc -DryRun

# Apply manifest (execute changes)
.\bin\cli.ps1 -Command apply -Manifest manifests\my-machine.jsonc

# Restore configurations (opt-in)
.\bin\cli.ps1 -Command restore -Manifest manifests\my-machine.jsonc -EnableRestore

# Verify desired state
.\bin\cli.ps1 -Command verify -Manifest manifests\my-machine.jsonc

# Check environment health
.\bin\cli.ps1 -Command doctor

# View run history
.\bin\cli.ps1 -Command report -Latest

# Compare artifacts
.\bin\cli.ps1 -Command diff -FileA plans\run1.json -FileB plans\run2.json
```

## Architecture

### Data Flow
```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

### Core Components

**engine/** - Orchestration logic (all operations flow through here)
- `capture.ps1` - Captures current machine state via winget export, generates manifests
- `plan.ps1` - Resolves manifests into executable plans, computes diff against current state
- `apply.ps1` - Executes plans: installs apps, applies configs, runs verifications
- `verify.ps1` - Confirms desired state matches reality
- `restore.ps1` - Applies configuration files (opt-in for safety)
- `manifest.ps1` - Manifest parsing (JSONC/JSON/YAML), include resolution, circular detection
- `state.ps1` - Persists run history for drift detection
- `report.ps1` - Formats human/machine-readable reports
- `diff.ps1` - Compares plans/runs
- `discovery.ps1` - Detects installed software not managed by winget
- `config-modules.ps1` - Config module catalog system
- `parallel.ps1` - Parallel execution primitives
- `logging.ps1` - Structured logging
- `progress.ps1` - Progress reporting

**drivers/** - Platform-specific package managers
- `winget.ps1` - Windows package installation adapter (primary driver)

**restorers/** - Configuration restoration modules
- `copy.ps1` - File copy with backup
- `append.ps1` - Append content to files
- `merge-json.ps1` - JSON merge strategies
- `merge-ini.ps1` - INI merge strategies
- `helpers.ps1` - Shared utilities (path expansion, backup)

**verifiers/** - State verification modules
- `file-exists.ps1` - Verify file existence
- `command-exists.ps1` - Verify command availability
- `registry-key-exists.ps1` - Verify registry state

**modules/** - Config module catalog
- `modules/apps/` - App-specific configuration modules (e.g., apps.git, apps.vscodium)

**manifests/** - Desired state declarations
- `manifests/examples/` - Shareable example manifests
- `manifests/includes/` - Reusable manifest fragments
- `manifests/local/` - Machine-specific captures (gitignored)

### Manifest System

Manifests use JSONC (JSON with comments) for human authoring. All plans, state, and reports are JSON.

**Supported formats:** `.jsonc` (preferred), `.json`, `.yaml`, `.yml`

**Include mechanism:** Manifests can include other manifests via relative paths. Arrays (apps, restore, verify) are concatenated. Circular includes are detected and rejected. Includes are resolved by `engine/manifest.ps1:Resolve-ManifestIncludes`.

**Config modules:** Apps in manifests can reference config modules (e.g., `"configModules": ["apps.git"]`). Modules expand into restore/verify items via `engine/config-modules.ps1:Expand-ManifestConfigModules`.

### State and Plans

**plans/** - Generated execution plans (timestamped JSON)
**state/** - Run history and backups
- `state/capture/<runId>/` - Capture intermediates
- `state/backups/<timestamp>/` - File backups before overwrite
- `state/*.json` - Run state records (used for drift detection, report command)

## Development Guidelines

### Code Patterns

**Error Handling:** Use `$ErrorActionPreference = "Stop"` at script top. Wrap risky operations in try/catch. Return structured results with `Success`, `Error`, and optional `Message` properties.

**Logging:** Use functions from `engine/logging.ps1`:
- `Write-ProvisioningLog -Level INFO|SUCCESS|WARN|ERROR|ACTION|SKIP`
- `Write-ProvisioningSection "Section Name"`
- `Initialize-ProvisioningLog -RunId "..."`
- `Close-ProvisioningLog -SuccessCount X -SkipCount Y -FailCount Z`

**Idempotence:** All operations must be safe to re-run. Check state before acting. Skip if already satisfied. Example: in `plan.ps1`, apps are marked "skip" if already installed.

**Non-Destructive:** Backup before overwrite. Use `restorers/helpers.ps1:Backup-File` which creates timestamped backups in `state/backups/`. No deletions unless explicit.

**State Management:** Use `engine/state.ps1` functions:
- `Save-RunState` - Persist execution results
- `Get-RunId` - Generate RFC3339 timestamp IDs
- `Get-ManifestHash` - SHA256 hash for drift detection

**Testing:** Follow existing Pester patterns in `tests/unit/`. Use fixtures from `tests/fixtures/`. Tests should be fast, deterministic, and offline-capable.

### Module Loading

PowerShell modules are dot-sourced at the top of files that depend on them. Example pattern:
```powershell
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\..\drivers\winget.ps1"
```

### Manifest Processing Pipeline

1. **Read-Manifest** (`engine/manifest.ps1`) - Entry point, handles includes and config module expansion
2. **Read-ManifestInternal** - Recursive loader with circular detection
3. **ConvertFrom-Jsonc** / **ConvertFrom-SimpleYaml** - Format parsers
4. **Resolve-ManifestIncludes** - Merge included manifests
5. **Expand-ManifestConfigModules** - Expand config module references into restore/verify items
6. **Normalize-Manifest** - Ensure required fields exist

### Testing Infrastructure

Pester 5.7.1 is vendored in `tools/pester/` for deterministic, offline-capable testing. The `scripts/ensure-pester.ps1` script handles bootstrap. CI runs `scripts/test_pester.ps1 -Path tests/unit` via GitHub Actions (`.github/workflows/ci.yml`).

## Important Constraints

**Windows-first:** While designed platform-agnostic, implementation is currently Windows-only. Uses winget as primary driver. Future: apt/brew support planned.

**Opt-in restore:** Configuration restoration is disabled by default. Requires explicit `-EnableRestore` flag for safety.

**No runtime dependencies:** Pure PowerShell (5.1+). Only external dependency is winget for app installation.

**Sensitive paths:** `engine/capture.ps1` defines `$script:SensitivePaths` array. Never auto-export SSH keys, credentials, browser profiles, etc.

**Runtime filtering:** By default, capture excludes runtime/framework packages (VCRedist, .NET runtimes) unless `-IncludeRuntimes` specified. Patterns in `$script:RuntimePatterns`.

**Manifest versioning:** Manifests have a `version` field (currently `1`). This enables future breaking changes with migration paths.

## Safety Principles

These principles apply to ALL code changes:

1. **Idempotent by default** - Running twice must not duplicate work or corrupt state
2. **Declarative desired state** - Describe what should be true, not how to do it
3. **Non-destructive defaults** - Backup before overwrite, no silent deletions
4. **Verification-first** - "It ran" ≠ success. Success means desired state is observable
5. **Separation of concerns** - Capture ≠ plan ≠ apply ≠ verify. No stage assumes success of prior stage
6. **Auditable by humans** - Reports and logs must be readable and inspectable

## Project Context

Endstate was originally the `provisioning/` subsystem in automation-suite repository. Split into standalone project in 2025. Full git history preserved. Licensed under Apache 2.0.

Author: Hugo Ander Kivi (Substrate Systems OÜ)
