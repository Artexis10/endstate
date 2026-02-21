# AGENTS.md

Specialized agent definitions for the Endstate repository.

Each agent is scoped to a specific development workflow. All agents operate under the governance hierarchy:

1. `docs/ai/AI_CONTRACT.md` — global AI behavior contract (highest authority)
2. `docs/ai/PROJECT_SHADOW.md` — architectural truth, invariants, landmines
3. `docs/ai/PROJECT_RULES.md` — operational policy

---

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
```powershell
# Validate module loads without errors
$env:ENDSTATE_ALLOW_DIRECT = '1'
.\bin\endstate.ps1 capture --dry-run --json 2>&1 | Select-Object -Last 1
```

### Common Mistakes

- Forgetting `"optional": true` on restore entries (causes failure if payload not yet captured)
- Using `\` instead of `\\` in JSON paths
- Missing `excludeGlobs` for cache/log directories (bloats capture bundles)
- Not creating matching `payload/apps/<id>/` directory
- Putting `dest` paths outside the `apps/<id>/` prefix

---

## TestWriter

**Role:** Write Pester 5 unit tests in `tests/unit/`.

**When to use:** Adding test coverage for engine changes, locking regression behavior, verifying contract compliance.

### Context

Tests use Pester 5.7.1 vendored in `tools/pester/`. System Pester may be 3.x — never call `Invoke-Pester` directly. All tests must be hermetic: no real winget calls, no network access, no filesystem side effects outside temp directories.

### Test File Convention

- Location: `tests/unit/<Subject>.Tests.ps1`
- Naming: PascalCase subject matching the engine module or concept being tested
- Fixtures: `tests/fixtures/` for test manifests, plans, module definitions

### Test Structure Pattern

```powershell
<#
.SYNOPSIS
    Pester tests for <subject>.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\..\"
    # Load the module(s) under test
    . (Join-Path $script:ProvisioningRoot "engine\<module>.ps1")
}

Describe "<FunctionOrConcept>" {

    Context "<scenario>" {

        It "Should <expected behavior>" {
            # Arrange
            $input = ...

            # Act
            $result = Invoke-Something -Param $input

            # Assert
            $result | Should -Be $expected
        }
    }
}
```

### Mocking External Dependencies

```powershell
# Mock winget calls
Mock Invoke-WingetInstall { return @{ ExitCode = 0; Output = "Successfully installed" } }

# Mock file system for path existence
Mock Test-Path { return $true } -ParameterFilter { $Path -like "*\some\path*" }

# Mock Read-JsoncFile for manifest loading
Mock Read-JsoncFile { return @{ version = 1; apps = @() } }
```

### Key Rules

- Use vendored Pester: run via `.\scripts\test-unit.ps1`, never bare `Invoke-Pester`
- No real installs, no network, no host mutation
- Use `$TestDrive` for temp files (Pester auto-cleans)
- Prefer `Should -Be`, `Should -BeExactly`, `Should -Match` over `Should -BeTrue`
- Tag tests with `[Tag("Unit")]` when appropriate
- Co-locate fixtures in `tests/fixtures/` — never reference `manifests/local/`

### Existing Test Files (for pattern reference)

| File | Tests |
|------|-------|
| `Manifest.Tests.ps1` | Manifest parsing, include resolution, circular detection |
| `Plan.Tests.ps1` | Plan generation, diff computation |
| `Events.Tests.ps1` | Streaming event emission and schema validation |
| `Verify.Tests.ps1` | All three verifier types (file-exists, command-exists, registry-key-exists) |
| `Restore.Tests.ps1` | Restore strategies (copy, merge-json, merge-ini, append) |
| `Capture.Tests.ps1` | Capture pipeline and artifact generation |
| `JsonSchema.Tests.ps1` | JSON envelope structure validation |
| `ProfileContract.Tests.ps1` | Profile validation contract compliance |
| `Bundle.Tests.ps1` | Bundle loading and module grouping |

### Verification

```powershell
# Run specific test file
.\scripts\test-unit.ps1 -Path tests\unit\<Subject>.Tests.ps1

# Run all unit tests
.\scripts\test-unit.ps1
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
- [ ] No direct `ConvertFrom-Json` on manifests (must use `Read-JsoncFile`)
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

```powershell
# OpenSpec validation
npm run openspec:validate

# Unit tests (contract subset)
.\scripts\test-unit.ps1 -Path tests\unit\JsonSchema.Tests.ps1
.\scripts\test-unit.ps1 -Path tests\unit\ProfileContract.Tests.ps1
.\scripts\test-unit.ps1 -Path tests\unit\Events.Tests.ps1
```

---

## EngineDev

**Role:** Implement and modify core engine scripts, drivers, restorers, and verifiers.

**When to use:** Core pipeline development — changes to apply, capture, verify, plan, restore, events, state, or manifest processing.

### Context

The engine is the authoritative layer of Endstate. All business logic lives here. The GUI is a thin presentation layer that consumes engine output. The engine pipeline is:

```
Manifest → Planner → Drivers → Restorers → Verifiers → Reports/State
```

### Key Engine Files

| File | Responsibility |
|------|----------------|
| `engine/apply.ps1` | Apply orchestration (install + optional restore + verify) |
| `engine/capture.ps1` | System state capture to manifest |
| `engine/verify.ps1` | State verification against manifest |
| `engine/plan.ps1` | Execution plan generation |
| `engine/manifest.ps1` | Manifest loading, include resolution, JSONC parsing |
| `engine/config-modules.ps1` | Module catalog loading, validation, manifest expansion |
| `engine/restore.ps1` | Restore orchestration |
| `engine/events.ps1` | Streaming event emission (JSONL to stderr) |
| `engine/json-output.ps1` | JSON envelope construction for `--json` output |
| `engine/state.ps1` | State persistence (atomic writes) |
| `engine/diff.ps1` | Diff computation between manifest and current state |
| `engine/parallel.ps1` | Parallel installation orchestration |
| `engine/paths.ps1` | Path resolution and environment variable expansion |
| `engine/snapshot.ps1` | System snapshot for capture |
| `engine/export-capture.ps1` | Export configuration from system |
| `engine/export-validate.ps1` | Validate export integrity |
| `engine/export-revert.ps1` | Revert last restore (journal-based) |
| `engine/profile-commands.ps1` | Profile CLI subcommands |

### Drivers

| File | Purpose |
|------|---------|
| `drivers/driver.ps1` | Driver interface/registry |
| `drivers/winget.ps1` | winget package manager adapter |

### Restorers

| File | Strategy |
|------|----------|
| `restorers/copy.ps1` | File/directory copy with backup |
| `restorers/merge-json.ps1` | JSON merge (preserve + overlay) |
| `restorers/merge-ini.ps1` | INI file merge |
| `restorers/append.ps1` | Append to existing file |
| `restorers/helpers.ps1` | Shared restore utilities |

### Verifiers

| File | Check |
|------|-------|
| `verifiers/file-exists.ps1` | File or directory existence |
| `verifiers/command-exists.ps1` | Command available on PATH |
| `verifiers/registry-key-exists.ps1` | Registry key/value existence |

### Landmines

1. **Entrypoint guard:** `bin/endstate.ps1` blocks direct invocation. Set `$env:ENDSTATE_ALLOW_DIRECT='1'` for dev, or re-bootstrap after edits
2. **JSONC parsing:** ALWAYS use `Read-JsoncFile`. Raw `ConvertFrom-Json` on `.jsonc` files will fail on comments
3. **`-EnableRestore` wiring:** Must be explicitly threaded from CLI entry → `Invoke-ApplyCore`. Missing wiring silently skips all restore entries with no error
4. **Capture zip path rewriting:** `New-CaptureBundle` stages under `configs/<module-id>/` but modules reference `./payload/apps/<id>/`. Stage 2b rewrites paths
5. **`Copy-Item -Recurse` nesting:** When destination exists, PowerShell copies source INSIDE dest. Must `Remove-Item` dest first for idempotent directory copies
6. **State atomicity:** State writes use temp file + move (`Move-Item`) for atomic updates. Never write directly to `state.json`
7. **Line ending normalization:** Hash computation normalizes CRLF→LF. If you compute hashes, use the same normalization
8. **PowerShell 5.1 null handling:** `$null -eq $value` (not `$value -eq $null`) to avoid array comparison. Use `.ContainsKey()` not truthy/falsy for hashtable lookups
9. **Events to stderr:** `Write-StreamingEvent` uses `[Console]::Error.WriteLine()`, not `Write-Error`. Events are informational, not error streams
10. **Module catalog caching:** `Get-ConfigModuleCatalog` caches on first load. Pass `-Force` to reload after dynamic changes
11. **PATH bootstrap:** Bootstrap installs to `%LOCALAPPDATA%\Endstate\bin\lib\` (not `bin\` directly). The CMD shim at `bin\endstate.cmd` must take precedence over `.ps1`
12. **Exit code capture:** In PowerShell 5.1, `$LASTEXITCODE` is the only reliable way to capture process exit codes. `$?` is unreliable for external commands

### Development Workflow

```powershell
# Test against repo code (bypass bootstrap)
$env:ENDSTATE_ALLOW_DIRECT = '1'
.\bin\endstate.ps1 <command> --json

# Run targeted unit tests after changes
.\scripts\test-unit.ps1 -Path tests\unit\<Subject>.Tests.ps1

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

### Validation Script

```powershell
# Load all modules and check for load errors
$env:ENDSTATE_ALLOW_DIRECT = '1'
.\bin\endstate.ps1 capture --dry-run --json 2>&1

# Run module-related unit tests
.\scripts\test-unit.ps1 -Path tests\unit\Capture.Tests.ps1
.\scripts\test-unit.ps1 -Path tests\unit\ModuleRestore.Tests.ps1
.\scripts\test-unit.ps1 -Path tests\unit\ModuleCli.Tests.ps1
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
