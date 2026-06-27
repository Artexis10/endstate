# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Endstate is a declarative system provisioning and recovery tool for Windows. It eliminates the "clean install tax" by enabling repeatable machine rebuilds from a single manifest. Primary language is Go; the engine lives in `go-engine/`.

## Governance Documents (Read These First)

This repo has an explicit authority hierarchy for AI collaborators:

1. `docs/ai/AI_CONTRACT.md` — AI behavior contract (highest authority)
2. `docs/ai/PROJECT_RULES.md` — operational policy (env vars, testing, protected areas)
3. `CLAUDE.md` — architecture context, commands, landmines (this file, auto-loaded by Claude Code)
4. `openspec/specs/` — invariants and behavior specifications (lazy-loaded on demand)

Key rules: make the smallest change satisfying acceptance criteria; no unrelated refactors or formatting sweeps; contract-first edits (schema → implementation → tests); significant changes must be represented in OpenSpec specs.

## Commands

```bash
# Run all unit tests (canonical verification command)
cd go-engine && go test ./...

# Run a specific test package
cd go-engine && go test ./internal/manifest/...

# Validate OpenSpec specs
npm run openspec:validate

# Install git hooks (lefthook pre-push)
npm run hooks:install

# Build and run CLI in dev mode
cd go-engine && go run ./cmd/endstate <command>
```

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

- **`go-engine/cmd/endstate/`** — CLI entrypoint (Go binary)
- **`go-engine/internal/`** — Core engine packages:
  - `manifest/` — Manifest loading, include resolution, JSONC stripping
  - `commands/` — CLI command implementations (apply, capture, restore, verify, etc.)
  - `modules/` — Config module catalog loading, validation, manifest expansion
  - `planner/` — Execution plan generation and diff computation
  - `driver/` — Package manager adapters (winget is primary)
  - `restore/` — Config restoration strategies (copy, merge-json, merge-ini, append)
  - `verifier/` — State assertions (file-exists, command-exists, registry-key-exists)
  - `events/` — Streaming event emission (JSONL)
  - `envelope/` — JSON output envelope construction
  - `snapshot/` — System snapshot for capture
  - `config/` — Configuration and path resolution
  - `bundle/` — Bundle loading and module grouping
- **`modules/apps/<id>/module.jsonc`** — Reusable config module definitions with matches, verify, restore, capture sections
- **`payload/apps/<id>/`** — Staged configuration files referenced by modules
- **`bundles/`** — Named module groupings (JSONC)
- **`manifests/`** — Desired state declarations (`examples/` shareable, `includes/` reusable fragments, `local/` gitignored machine-specific)

## Critical Landmines

1. **JSONC parsing**: Always use `StripJsoncComments` (in `go-engine/internal/manifest/`) — never raw `json.Unmarshal` on `.jsonc` files
2. **Capture zip layout**: Capture stages files under `configs/<module-id>/` but module definitions use `./payload/apps/<id>/` — path rewriting must reconcile these
3. **Directory copy nesting**: When copying directories, ensure the destination is removed first for idempotent behavior — copying into an existing directory nests the source inside it
4. **Line endings**: Manifest hashes normalize CRLF→LF for cross-platform consistency
5. **Stale bootstrapped copies**: The GUI (and PATH-based invocations) run the bootstrapped copy at `%LOCALAPPDATA%\Endstate\bin\`, not the repo. New engine features won't appear in the GUI until re-bootstrapped. Always run `endstate bootstrap` after engine changes. The GUI's `predev` npm hook handles this for `npm run dev` / `tauri dev`.
6. **Winget database lock contention**: Concurrent winget operations (or rapid successive calls) can fail due to SQLite lock contention on the winget database. Capture retries once on 0-app results to handle this.
7. **Batch vs per-ref display name differences**: `DetectBatch` returns display names from winget's local database which may differ from per-ref `winget show` output. Use batch results as authoritative for installed app names.
8. **Manual app `launch`/`instructions` are GUI metadata only**: The engine includes `launch` URLs and `instructions` text in manual app entries, but these are consumed exclusively by the GUI for display. The engine never opens URLs or displays instructions itself.

## Core Invariants

- **Idempotent**: Re-running converges without duplicating work
- **Non-destructive defaults**: No silent deletions; destructive ops require explicit flags
- **Restore is opt-in**: Requires `--enable-restore` flag
- **Verification-first**: Observable state is success, not "it ran"
- **Separation of concerns**: Install ≠ configure ≠ verify (distinct pipeline stages)
- **Backup before overwrite**: Files backed up to `state/backups/<timestamp>/`
- **CLI is source of truth**: GUI is thin presentation layer

## Testing

- **Framework**: Go standard `testing` package
- **Unit tests**: `go-engine/internal/*/` — all hermetic, CI-safe, no real winget calls; test files use `_test.go` suffix
- **Fixtures**: `tests/fixtures/` for shared test manifests and module definitions
- **CI**: GitHub Actions runs `cd go-engine && go test ./...` on windows-latest
- Run only minimum targeted verification needed; do not run full suite unless requested

## Protected Areas

- `go-engine/cmd/endstate/`, `docs/contracts/*.md`, `.github/workflows/` — require explicit instruction to modify
- `docs/ai/AI_CONTRACT.md`, `LICENSE`, `NOTICE` — never modify without explicit request

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_TESTMODE` | Enable test mode |

## OpenSpec

Behavior specs enforced at Level 2 (pre-push hook via lefthook). Specs live in `openspec/specs/`, changes in `openspec/changes/`. Emergency bypass: `OPENSPEC_BYPASS=1 git push`.

## Forbidden Patterns

- Hardcoded absolute paths (use relative paths or environment variables)
- Raw `json.Unmarshal` on `.jsonc` files (use `StripJsoncComments` first)
- Committing runtime artifacts (`logs/`, `plans/`, `state/`, `manifests/local/`)
- Bypassing git hooks (`--no-verify`)

---

## Specialized Agent Definitions

Specialized agent role playbooks for this repo. Pick the role matching the task. All operate under the governance hierarchy above, plus `docs/ai/PROJECT_SHADOW.md` (architectural truth, invariants, landmines).

## ModuleAuthor

**Role:** Create and maintain config module definitions in `modules/apps/*/module.jsonc`.

**When to use:** Adding new app modules, updating capture/restore configurations, expanding coverage for existing modules, creating seed scripts.

### Context

Config modules are the unit of configuration portability in Endstate. Each module defines how to detect, verify, capture, and restore an application's configuration. Modules live at `modules/apps/<app-id>/module.jsonc`. Associated payload files (staged configs) live at `payload/apps/<app-id>/`.

### Module Schema

Every module.jsonc MUST include:

```jsonc
{
  "id": "apps.<app-id>",           // REQUIRED. Format: apps.<kebab-case-id>
  "displayName": "<Human Name>",   // REQUIRED. User-facing name
  "sensitivity": "low",            // REQUIRED. One of: "none", "low", "medium", "high"

  "matches": {                     // REQUIRED. Detection criteria
    "winget": ["Vendor.PackageId"],  // Array of winget IDs (may be empty for manual-install apps)
    "exe": ["app.exe"],              // Executable names for process detection
    "uninstallDisplayName": ["^RegexPattern"]  // Regex patterns against Add/Remove Programs
  },

  "verify": [],     // Array of verification steps
  "restore": [],    // Array of restore entries
  "capture": {},    // Capture definition with files and excludeGlobs
  "sensitive": {},  // Optional. Files that MUST NOT be auto-restored
  "notes": "",      // Human-readable notes about scope and exclusions
  "curation": {}    // Optional. Seed script and snapshot configuration
}
```

### Verification Step Types

```jsonc
{ "type": "command-exists", "command": "code" }
{ "type": "file-exists", "path": "%APPDATA%\\App\\config.json" }
{ "type": "registry-key-exists", "path": "HKCU:\\Software\\App", "valueName": "Setting" }
```

### Restore Entry Format

```jsonc
{
  "type": "copy",                              // One of: copy, merge-json, merge-ini, append
  "source": "./payload/apps/<id>/file.json",   // Relative to manifest directory
  "target": "%APPDATA%\\App\\file.json",       // System path with env var expansion
  "backup": true,                              // Always true for safety
  "optional": true                             // true = no error if source missing
}
```

For directory copy with exclusions:
```jsonc
{
  "type": "copy",
  "source": "./payload/apps/<id>/Config",
  "target": "%LOCALAPPDATA%\\App\\Config",
  "backup": true,
  "optional": true,
  "exclude": ["**\\Logs\\**", "**\\Cache\\**", "**\\*.lock"]
}
```

### Capture Definition Format

```jsonc
"capture": {
  "files": [
    { "source": "%APPDATA%\\App\\settings.json", "dest": "apps/<id>/settings.json", "optional": true }
  ],
  "excludeGlobs": [
    "**\\Cache\\**",
    "**\\Logs\\**",
    "**\\*.lock"
  ]
}
```

### Sensitivity Classification

| Level | Meaning | Examples |
|-------|---------|---------|
| `none` | No user data involved | CLI tools with no config |
| `low` | User preferences, no credentials | Editor settings, keybindings |
| `medium` | May contain indirect credential refs | Password manager settings (NOT databases) |
| `high` | Contains or accesses credentials directly | SSH configs, token stores |

### Security Rules (Non-Negotiable)

- NEVER include browser profiles, auth tokens, password databases, or license blobs in capture/restore
- The `sensitive` section documents paths that MUST NOT be restored; set `"restorer": "warn-only"`
- Apps with `sensitivity: "high"` require explicit justification in `notes`
- `excludeGlobs` MUST exclude: Cache, Logs, Temp, lock files, crash reports, GPU cache, state databases (`.vscdb`)

### Capture ↔ Restore Symmetry

- Every `capture.files[].dest` path MUST have a corresponding `restore[]` entry with matching `source`
- `capture.files[].source` (system path) maps to `restore[].target` (system path)
- Prefix in `dest` MUST be `apps/<module-id>/`

### Reference Examples

- Simple app (files + keybindings): `modules/apps/vscode/module.jsonc`
- Complex app (26 restore entries): `modules/apps/lightroom-classic/module.jsonc`
- Medium sensitivity: `modules/apps/keepassxc/module.jsonc`
- No winget ID (manual install): `modules/apps/lightroom-classic/module.jsonc` (empty `winget: []`)

### Verification

After creating/modifying a module:
```bash
# Validate module loads without errors
cd go-engine && go run ./cmd/endstate capture --dry-run --json
```

### Common Mistakes

- Forgetting `"optional": true` on restore entries (causes failure if payload not yet captured)
- Using `\` instead of `\\` in JSON paths
- Missing `excludeGlobs` for cache/log directories (bloats capture bundles)
- Not creating matching `payload/apps/<id>/` directory
- Putting `dest` paths outside the `apps/<id>/` prefix

---

## TestWriter

**Role:** Write Go unit tests in `go-engine/internal/`.

**When to use:** Adding test coverage for engine changes, locking regression behavior, verifying contract compliance.

### Context

Tests use Go's standard `testing` package. All tests must be hermetic: no real winget calls, no network access, no filesystem side effects outside temp directories.

### Test File Convention

- Location: `go-engine/internal/<package>/<subject>_test.go`
- Naming: snake_case file name with `_test.go` suffix, in the same package as the code under test
- Fixtures: `tests/fixtures/` for test manifests, plans, module definitions

### Test Structure Pattern

```go
package manifest

import (
    "testing"
)

func TestLoadManifest(t *testing.T) {
    // Table-driven tests
    tests := []struct {
        name    string
        input   string
        want    *Manifest
        wantErr bool
    }{
        {
            name:  "valid manifest",
            input: `{"version": 1, "name": "test"}`,
            want:  &Manifest{Version: 1, Name: "test"},
        },
        {
            name:    "invalid JSON",
            input:   `{invalid}`,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := LoadManifest(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("LoadManifest() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            // assertions...
        })
    }
}
```

### Key Rules

- No real installs, no network, no host mutation
- Use `t.TempDir()` for temp files (Go auto-cleans)
- Prefer table-driven tests for comprehensive coverage
- Use `t.Run()` subtests for clear test case naming
- Co-locate fixtures in `tests/fixtures/` -- never reference `manifests/local/`

### Existing Test Packages (for pattern reference)

| Package | Tests |
|---------|-------|
| `go-engine/internal/manifest/` | Manifest parsing, include resolution, JSONC stripping |
| `go-engine/internal/commands/` | Command implementations (restore, etc.) |
| `go-engine/internal/modules/` | Module catalog loading, validation, expansion |

### Verification

```bash
# Run specific package tests
cd go-engine && go test ./internal/<package>/...

# Run all unit tests
cd go-engine && go test ./...
```

---

## ContractGuard

**Role:** Review changes against contracts, specs, and invariants. Identify violations before they ship.

**When to use:** Code review, pre-push compliance checks, verifying that engine or module changes don't violate established contracts.

### Context

Endstate has 7 contracts, 3 OpenSpec specs, 10+ invariants, and a 3-layer governance hierarchy. Changes that touch engine behavior, CLI output, event emission, or module schema may violate one or more of these. ContractGuard systematically checks for violations.

### Contract Inventory

| Contract | Path | Governs |
|----------|------|---------|
| CLI JSON Contract | `docs/contracts/cli-json-contract.md` | `--json` output envelope, error codes, schema versioning |
| GUI Integration | `docs/contracts/gui-integration-contract.md` | GUI ↔ CLI integration rules, capabilities handshake |
| Event Contract | `docs/contracts/event-contract.md` | JSONL streaming events, phase/item/summary schema |
| Profile Contract | `docs/contracts/profile-contract.md` | Profile validation, discovery, display label resolution |
| Config Portability | `docs/contracts/config-portability-contract.md` | Export/restore symmetry, journal, revert semantics |
| Capture Artifact | `docs/contracts/capture-artifact-contract.md` | Capture success/failure invariants |
| Restore Safety | `docs/contracts/restore-safety-contract.md` | Backup-before-overwrite, opt-in restore |

### OpenSpec Specs

| Spec | Path | Governs |
|------|------|---------|
| Capture Artifact | `openspec/specs/capture-artifact-contract.md` | Capture success implies valid artifact |
| Capture Bundle Zip | `openspec/specs/capture-bundle-zip.md` | Zip layout and path rewriting |
| Profile Composition | `openspec/specs/profile-composition.md` | Profile include resolution |

### Core Invariants (from PROJECT_SHADOW.md)

1. Idempotence — re-running converges without duplicating work
2. Non-destructive defaults — no silent deletions
3. Verification-first — observable state is success
4. Separation of concerns — install ≠ configure ≠ verify
5. Backup before overwrite
6. Restore is opt-in (`-EnableRestore`)
7. CLI is source of truth
8. JSON schema versioning

### Review Checklist

When reviewing a change, verify:

- [ ] JSON envelope fields unchanged (or schema version bumped if changed)
- [ ] Event emission follows schema v1 (required fields: version, runId, timestamp, event)
- [ ] First event is phase, last event is summary
- [ ] Status/reason combinations match `docs/ux-language.md` (cross-repo)
- [ ] No business logic added to GUI
- [ ] No raw `json.Unmarshal` on `.jsonc` files (must use `StripJsoncComments` first)
- [ ] Restore entries have `backup: true`
- [ ] No secrets/credentials in capture/restore
- [ ] Error codes use SCREAMING_SNAKE_CASE from the standard set
- [ ] CLI flag changes reflected in capabilities command
- [ ] No hardcoded absolute paths
- [ ] State writes use temp + atomic move pattern

### Cross-Repo Coupling

Status/phase semantics are coupled between engine and GUI:
- Engine side: `docs/contracts/event-contract.md`
- GUI side: `endstate-gui/docs/ux-language.md`

Changes to status, reason, or phase behavior MUST update both repos.

### Validation Commands

```bash
# OpenSpec validation
npm run openspec:validate

# Run all unit tests
cd go-engine && go test ./...
```

---

## EngineDev

**Role:** Implement and modify core engine packages in `go-engine/internal/`.

**When to use:** Core pipeline development -- changes to apply, capture, verify, plan, restore, events, state, or manifest processing.

### Context

The engine is the authoritative layer of Endstate. All business logic lives here. The GUI is a thin presentation layer that consumes engine output. The engine pipeline is:

```
Manifest -> Planner -> Drivers -> Restorers -> Verifiers -> Reports/State
```

### Key Engine Packages

| Package | Responsibility |
|---------|----------------|
| `go-engine/cmd/endstate/` | CLI entrypoint |
| `go-engine/internal/commands/` | Command implementations (apply, capture, restore, verify, etc.) |
| `go-engine/internal/manifest/` | Manifest loading, include resolution, JSONC stripping |
| `go-engine/internal/modules/` | Module catalog loading, validation, manifest expansion |
| `go-engine/internal/planner/` | Execution plan generation and diff computation |
| `go-engine/internal/driver/` | Package manager adapters (winget is primary) |
| `go-engine/internal/restore/` | Config restoration strategies (copy, merge-json, merge-ini, append) |
| `go-engine/internal/verifier/` | State assertions (file-exists, command-exists, registry-key-exists) |
| `go-engine/internal/events/` | Streaming event emission (JSONL to stderr) |
| `go-engine/internal/envelope/` | JSON envelope construction for `--json` output |
| `go-engine/internal/snapshot/` | System snapshot for capture |
| `go-engine/internal/config/` | Configuration and path resolution |
| `go-engine/internal/bundle/` | Bundle loading and module grouping |

### Landmines

1. **JSONC parsing:** Always use `StripJsoncComments` before `json.Unmarshal`. Raw unmarshal on `.jsonc` files will fail on comments
2. **Capture zip path rewriting:** Capture stages under `configs/<module-id>/` but modules reference `./payload/apps/<id>/`. Path rewriting must reconcile these
3. **Directory copy nesting:** When copying directories, ensure the destination is removed first for idempotent behavior
4. **State atomicity:** State writes use temp file + rename for atomic updates. Never write directly to `state.json`
5. **Line ending normalization:** Hash computation normalizes CRLF to LF. If you compute hashes, use the same normalization
6. **Events to stderr:** Streaming events are written to stderr, not stdout. Events are informational, not error streams
7. **PATH bootstrap:** Bootstrap installs to `%LOCALAPPDATA%\Endstate\bin\`. Re-bootstrap after engine changes

### Development Workflow

```bash
# Run CLI from source
cd go-engine && go run ./cmd/endstate <command> --json

# Run targeted unit tests after changes
cd go-engine && go test ./internal/<package>/...

# Validate OpenSpec compliance
npm run openspec:validate
```

### Contract-First Edit Pattern

For behavior changes:
1. Update contract document (`docs/contracts/`) if affected
2. Add/update OpenSpec spec if behavior semantics change
3. Implement the change in engine
4. Add/update unit test
5. Verify with targeted test run

---

## ModuleValidator

**Role:** Audit modules for schema compliance, path validity, safety violations, and cross-module consistency.

**When to use:** Batch validation of the module catalog, pre-release audits, after bulk module additions.

### Context

The module catalog at `modules/apps/` currently contains 72 modules. Each must conform to the module schema, maintain capture↔restore symmetry, correctly classify sensitivity, and exclude dangerous paths. This agent performs systematic validation across the entire catalog.

### Validation Checks

#### Schema Compliance

For each `modules/apps/*/module.jsonc`:
- [ ] Has `id` field matching `apps.<directory-name>` pattern
- [ ] Has `displayName` (non-empty string)
- [ ] Has `sensitivity` field with valid value: `none`, `low`, `medium`, `high`
- [ ] Has `matches` object with `winget` (array), `exe` (array), `uninstallDisplayName` (array)
- [ ] Has `verify` array (may be empty)
- [ ] Has `restore` array (may be empty)
- [ ] Has `capture` object with `files` array

#### Capture ↔ Restore Symmetry

For each module with both `capture.files` and `restore` entries:
- [ ] Every `capture.files[].dest` has a corresponding `restore[]` entry
- [ ] `capture.files[].dest` paths use `apps/<id>/` prefix
- [ ] Restore `source` paths reference `./payload/apps/<id>/`

#### Safety Audit

- [ ] No modules capture browser profile directories
- [ ] No modules capture credential stores, token files, or `.ssh/id_*` private keys
- [ ] Modules with `sensitivity: "medium"` or `"high"` have a `sensitive` section documenting excluded paths
- [ ] `sensitive.restorer` is `"warn-only"` (never `"auto"`)
- [ ] `excludeGlobs` include at minimum: `**\\Cache\\**`, `**\\Logs\\**` for apps with AppData storage

#### Path Validity

- [ ] All restore `target` paths use `%APPDATA%`, `%LOCALAPPDATA%`, `%USERPROFILE%`, or other standard env vars (no hardcoded `C:\Users\*`)
- [ ] All capture `source` paths use environment variables for user-specific paths
- [ ] No paths reference `ProgramData` or `HKLM` without explicit justification

#### Consistency

- [ ] Module `id` field matches directory name: `apps.<dirname>`
- [ ] No duplicate winget IDs across modules
- [ ] No duplicate module IDs
- [ ] All restore entries have `backup: true`

### Validation Commands

```bash
# Load all modules and check for load errors
cd go-engine && go run ./cmd/endstate capture --dry-run --json

# Run module-related unit tests
cd go-engine && go test ./internal/modules/...
cd go-engine && go test ./internal/commands/...
```

### Expected Output

Report format:

```
Module Validation Report
========================
Total modules scanned: 72
Schema valid: 70
Schema errors: 2
  - apps.foo: missing 'sensitivity' field
  - apps.bar: 'id' doesn't match directory name

Safety violations: 1
  - apps.baz: captures browser profile directory

Symmetry mismatches: 3
  - apps.qux: capture dest 'apps/qux/config.json' has no restore entry

Path issues: 0
Duplicate IDs: 0
```
