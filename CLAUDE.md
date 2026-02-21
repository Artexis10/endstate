# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Endstate is a declarative system provisioning and recovery tool for Windows. It eliminates the "clean install tax" by enabling repeatable machine rebuilds from a single manifest. Primary language is PowerShell; no build step required.

## Governance Documents (Read These First)

This repo has an explicit authority hierarchy for AI collaborators:

1. `docs/ai/AI_CONTRACT.md` — global AI behavior contract (highest authority)
2. `docs/ai/PROJECT_SHADOW.md` — architectural truth, invariants, landmines
3. `docs/ai/PROJECT_RULES.md` — operational policy (env vars, testing, protected areas)

Key rules: make the smallest change satisfying acceptance criteria; no unrelated refactors or formatting sweeps; contract-first edits (schema → implementation → tests); behavior changes must be represented in OpenSpec specs.

## Commands

```powershell
# Run all unit tests (canonical verification command)
.\scripts\test-unit.ps1

# Run specific test file
.\scripts\test-unit.ps1 -Path tests\unit\Manifest.Tests.ps1

# Run tests with tag filter (uses legacy runner)
.\scripts\test_pester.ps1 -Tag "Manifest"

# Validate OpenSpec specs
npm run openspec:validate

# Install git hooks (lefthook pre-push)
npm run hooks:install

# Run CLI in dev mode (bypass entrypoint guard)
$env:ENDSTATE_ALLOW_DIRECT = '1'
.\bin\endstate.ps1 <command>
```

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

- **`bin/endstate.ps1`** — CLI entrypoint (must be invoked via `endstate.cmd` shim in production; set `$env:ENDSTATE_ALLOW_DIRECT='1'` for dev)
- **`engine/`** — Core orchestration: manifest loading, apply, capture, plan, restore, verify, state persistence
- **`drivers/`** — Package manager adapters (winget is primary)
- **`restorers/`** — Config restoration strategies: copy, merge-json, merge-ini, append
- **`verifiers/`** — State assertions: file-exists, command-exists, registry-key-exists
- **`modules/apps/<id>/module.jsonc`** — Reusable config module definitions with matches, verify, restore, capture sections
- **`payload/apps/<id>/`** — Staged configuration files referenced by modules
- **`bundles/`** — Named module groupings (JSONC)
- **`manifests/`** — Desired state declarations (`examples/` shareable, `includes/` reusable fragments, `local/` gitignored machine-specific)

## Critical Landmines

1. **Entrypoint guard**: `endstate.ps1` blocks direct invocation unless `$env:ENDSTATE_ALLOW_DIRECT = '1'`
2. **JSONC parsing**: Always use `Read-JsoncFile` — never raw `ConvertFrom-Json` on manifests
3. **`-EnableRestore` wiring**: Flag must be explicitly passed through to `Invoke-ApplyCore`; missing wiring silently ignores restore entries
4. **Capture zip layout**: `New-CaptureBundle` stages files under `configs/<module-id>/` but module definitions use `./payload/apps/<id>/` — Stage 2b must rewrite source paths to match zip layout
5. **`Copy-Item -Recurse` nesting**: When destination exists, copies source *inside* dest. Must `Remove-Item` first for idempotent directory copies
6. **State atomicity**: State writes use temp file + move pattern
7. **Line endings**: Manifest hashes normalize CRLF→LF for cross-platform consistency

## Core Invariants

- **Idempotent**: Re-running converges without duplicating work
- **Non-destructive defaults**: No silent deletions; destructive ops require explicit flags
- **Restore is opt-in**: Requires `-EnableRestore` flag
- **Verification-first**: Observable state is success, not "it ran"
- **Separation of concerns**: Install ≠ configure ≠ verify (distinct pipeline stages)
- **Backup before overwrite**: Files backed up to `state/backups/<timestamp>/`
- **CLI is source of truth**: GUI is thin presentation layer

## Testing

- **Framework**: Pester 5.7.1 vendored in `tools/pester/` — do not use system Pester (may be 3.x)
- **Unit tests**: `tests/unit/` — all hermetic, CI-safe, no real winget calls
- **Contract tests**: `tests/contract/`
- **Fixtures**: `tests/fixtures/`
- **CI**: GitHub Actions runs `scripts/test_pester.ps1 -Path tests/unit` on windows-latest
- **Output**: `test-results.xml` (NUnitXml)
- Run only minimum targeted verification needed; do not run full suite unless requested

## Protected Areas

- `bin/endstate.ps1`, `docs/contracts/*.md`, `.github/workflows/` — require explicit instruction to modify
- `docs/ai/AI_CONTRACT.md`, `docs/ai/PROJECT_SHADOW.md`, `LICENSE`, `NOTICE` — never modify without explicit request

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_ALLOW_DIRECT` | Bypass entrypoint guard (set `1` for dev) |
| `ENDSTATE_TESTMODE` | Enable test mode |
| `ENDSTATE_ENTRYPOINT` | Set by CMD shim to verify invocation path |

## OpenSpec

Behavior specs enforced at Level 2 (pre-push hook via lefthook). Specs live in `openspec/specs/`, changes in `openspec/changes/`. Emergency bypass: `OPENSPEC_BYPASS=1 git push`.

## Forbidden Patterns

- Hardcoded absolute paths (use `$PSScriptRoot` or env vars)
- Direct `ConvertFrom-Json` on manifests (use `Read-JsoncFile`)
- Committing runtime artifacts (`logs/`, `plans/`, `state/`, `manifests/local/`)
- Bypassing git hooks (`--no-verify`)
