# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

Endstate is a declarative machine provisioning system written in Go. It eliminates the "clean install tax" by enabling safe, repeatable, auditable machine rebuilds from manifests. The system follows strict principles: declarative desired state, idempotence, non-destructive defaults, and verification-first design.

## Essential Commands

### Testing
```bash
# Run all tests
cd go-engine && go test ./...

# Run specific package tests
cd go-engine && go test ./internal/manifest/...

# Run CLI from source
cd go-engine && go run ./cmd/endstate <command> --json
```

### CLI Commands
```bash
# Capture current machine state
endstate capture --profile my-machine --json

# Generate execution plan
endstate plan --manifest manifests/my-machine.jsonc --json

# Dry-run (preview changes)
endstate apply --manifest manifests/my-machine.jsonc --dry-run --json

# Apply manifest (execute changes)
endstate apply --manifest manifests/my-machine.jsonc --json

# Restore configurations (opt-in)
endstate restore --manifest manifests/my-machine.jsonc --enable-restore --json

# Verify desired state
endstate verify --manifest manifests/my-machine.jsonc --json

# Check environment health
endstate doctor --json

# View run history
endstate report --json
```

## Architecture

### Data Flow
```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

### Core Packages (`go-engine/internal/`)

| Package | Purpose |
|---------|---------|
| `commands/` | CLI command implementations (apply, capture, restore, verify, etc.) |
| `manifest/` | Manifest loading, include resolution, JSONC stripping |
| `modules/` | Config module catalog loading, validation, manifest expansion |
| `planner/` | Execution plan generation and diff computation |
| `driver/` | Package manager adapters (winget is primary) |
| `restore/` | Config restoration strategies (copy, merge-json, merge-ini, append) |
| `verifier/` | State assertions (file-exists, command-exists, registry-key-exists) |
| `events/` | JSONL streaming events to stderr |
| `envelope/` | JSON output envelope construction |
| `snapshot/` | System snapshot for capture |
| `config/` | Configuration and path resolution |
| `bundle/` | Bundle loading and module grouping |
| `state/` | Run history persistence |

**modules/** - Config module catalog
- `modules/apps/` - App-specific configuration modules (e.g., apps.git, apps.vscode)

**manifests/** - Desired state declarations
- `manifests/examples/` - Shareable example manifests
- `manifests/includes/` - Reusable manifest fragments
- `manifests/local/` - Machine-specific captures (gitignored)

### Manifest System

Manifests use JSONC (JSON with comments) for human authoring. All plans, state, and reports are JSON.

**Supported formats:** `.jsonc` (preferred), `.json`

**Include mechanism:** Manifests can include other manifests via relative paths. Arrays (apps, restore, verify) are concatenated. Circular includes are detected and rejected.

**Config modules:** Apps in manifests can reference config modules (e.g., `"configModules": ["apps.git"]`). Modules expand into restore/verify items via `go-engine/internal/modules/expander.go:ExpandConfigModules`.

### State and Plans

**plans/** - Generated execution plans (timestamped JSON)
**state/** - Run history and backups
- `state/capture/<runId>/` - Capture intermediates
- `state/backups/<timestamp>/` - File backups before overwrite
- `state/*.json` - Run state records (used for drift detection, report command)

## Development Guidelines

### Code Patterns

**Idempotence:** All operations must be safe to re-run. Check state before acting. Skip if already satisfied.

**Non-Destructive:** Backup before overwrite. Files backed up to `state/backups/<timestamp>/`. No deletions unless explicit.

**Testing:** Follow existing Go test patterns in `go-engine/internal/`. Use table-driven tests, `t.TempDir()` for temp files. Tests must be hermetic — no real winget calls, no network access.

### JSONC Handling

Always use `manifest.StripJsoncComments()` before `json.Unmarshal` on `.jsonc` files. The module catalog loader handles this automatically.

## Important Constraints

**Windows-first:** While designed platform-agnostic, implementation is currently Windows-only. Uses winget as primary driver.

**Opt-in restore:** Configuration restoration is disabled by default. Requires explicit `--enable-restore` flag for safety.

**No runtime dependencies:** Single Go binary. Only external dependency is winget for app installation.

**Sensitive paths:** Capture excludes SSH keys, credentials, browser profiles, etc. via sensitive path detection.

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
