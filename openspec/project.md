# Endstate OpenSpec Project

Declarative system provisioning and recovery tool that restores a machine to a known-good end state safely, repeatably, and without guesswork.

**Author:** Hugo Ander Kivi  
**Primary Language:** PowerShell  
**Status:** Functional MVP — actively evolving

---

## Structure

- `specs/` — Behavior specifications
- `changes/` — Change proposals (if any)

## Enforcement

This repository enforces **Level 2** (workflow gate). See `docs/runbooks/OPENSPEC_ENFORCEMENT.md`.

---

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

| Stage | Responsibility |
|-------|----------------|
| **Spec** | Declarative manifest describing desired state (apps, configs, preferences) |
| **Planner** | Resolves spec into executable steps, detects drift, computes minimal diff |
| **Drivers** | Install software via platform-specific package managers (winget) |
| **Restorers** | Apply configuration files, registry keys, symlinks, preferences |
| **Verifiers** | Confirm desired state is achieved (file exists, app responds, config matches) |
| **Reports/State** | Persist run history, enable drift detection, provide human-readable logs |

---

## Key Directories

| Directory | Purpose |
|-----------|---------|
| `bin/` | CLI entrypoints (`endstate.ps1`, `endstate.cmd` shim, `cli.ps1`) |
| `engine/` | Core orchestration logic (manifest, apply, capture, restore, verify, snapshot, etc.) |
| `drivers/` | Software installation adapters (`winget.ps1` is primary) |
| `restorers/` | Configuration restoration modules (copy, merge-json, merge-ini, append) |
| `verifiers/` | State verification modules (file-exists, command-exists, registry-key-exists) |
| `modules/apps/` | Config module catalog (`<app-id>/module.jsonc`) |
| `bundles/` | Reusable module groupings (e.g., `core-utilities.jsonc`) |
| `manifests/` | Desired state declarations |
| `manifests/examples/` | Shareable example manifests |
| `manifests/includes/` | Reusable manifest fragments |
| `manifests/local/` | Machine-specific captures (gitignored) |
| `sandbox-tests/` | Sandbox-based discovery and validation harnesses |
| `tests/` | Pester unit tests (`unit/`, `contract/`, `fixtures/`) |
| `scripts/` | Test runners and utilities |
| `tools/pester/` | Vendored Pester 5.7.1 |
| `docs/contracts/` | Integration contracts (CLI JSON, GUI, events, profiles) |

---

## Entry Scripts

### CLI Entrypoint

- **`bin/endstate.ps1`** — Main CLI entrypoint (invoked via `endstate.cmd` shim)
- **`bin/endstate.cmd`** — Windows CMD wrapper for proper stdout/stderr handling

### Engine Core

| Script | Purpose |
|--------|---------|
| `engine/apply.ps1` | Execute plan and apply changes |
| `engine/capture.ps1` | Capture current machine state into manifest |
| `engine/verify.ps1` | Run verifiers without modifying state |
| `engine/snapshot.ps1` | Filesystem snapshot and diff helpers for sandbox discovery |
| `engine/restore.ps1` | Apply configuration restoration entries |
| `engine/manifest.ps1` | Manifest loading, includes resolution, module expansion |
| `engine/plan.ps1` | Plan generation from manifest diff |
| `engine/diff.ps1` | Diff computation between states |

### Sandbox Discovery Harness

| Script | Purpose |
|--------|---------|
| `scripts/sandbox-discovery.ps1` | Host-side entrypoint for sandbox-based module discovery |
| `sandbox-tests/discovery-harness/sandbox-install.ps1` | Sandbox-side install and capture script |
| `sandbox-tests/discovery-harness/sandbox-validate.ps1` | Sandbox-side validation script |
| `sandbox-tests/discovery-harness/curate.ps1` | Manual curation workflow |
| `sandbox-tests/discovery-harness/curate-git.ps1` | Git-specific curation script |
| `sandbox-tests/discovery-harness/curate-vscodium.ps1` | VSCodium-specific curation script |
| `sandbox-tests/discovery-harness/seed-git-config.ps1` | Git config seeding for sandbox |

---

## Catalog Layout

Endstate organizes configuration portability through three artifact types:

### Modules

**Location:** `modules/apps/<app-id>/module.jsonc`

Reusable configuration templates defining what to restore for a specific application.

```jsonc
{
  "id": "apps.<app-id>",
  "displayName": "App Name",
  "restore": [
    { "type": "copy", "source": "./configs/...", "target": "C:\\...", "backup": true }
  ]
}
```

**Current modules:** `git`, `msi-afterburner`, `powertoys`, `vscodium`

### Bundles

**Location:** `bundles/`

Group multiple modules into logical collections.

```jsonc
{
  "version": 1,
  "id": "core-utilities",
  "modules": ["msi-afterburner", "powertoys"]
}
```

### Manifests

**Location:** `manifests/`

Executable specifications consumed by the engine. Can reference bundles, modules, or contain inline restore entries.

**Resolution order:**
1. Bundle modules (in bundle order)
2. Manifest modules (in order)
3. Manifest inline `restore[]` (appended last)

---

## Sandbox Discovery Workflow

Automated module generation via Windows Sandbox:

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Sandbox        │────▶│  Discovery       │────▶│  Module Draft   │
│  Install        │     │  Diff Engine     │     │  Generator      │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                         │
                                                         ▼
                                                 ┌─────────────────┐
                                                 │  Human Review   │
                                                 │  + Enrichment   │
                                                 └─────────────────┘
```

### Usage

```powershell
# Discover and generate draft module
.\scripts\sandbox-discovery.ps1 -WingetId "Microsoft.PowerToys"

# Dry run (validate wiring only)
.\scripts\sandbox-discovery.ps1 -WingetId "Guru3D.Afterburner" -DryRun

# Write module directly to modules/apps/
.\scripts\sandbox-discovery.ps1 -WingetId "Git.Git" -WriteModule
```

### Artifacts Generated

- `pre.json` — Pre-install filesystem snapshot
- `post.json` — Post-install filesystem snapshot
- `diff.json` — Computed differences
- `module.jsonc` — Draft module for human review

---

## Golden App Curation Matrix

**Core Principle:** Endstate ONLY curates applications where restoring local state/configuration provides HIGH, irreplaceable user value.

**Target:** 30–50 apps MAX | Opinionated, defensible, premium list

### Assessment Criteria

| Criterion | Definition |
|-----------|------------|
| **Local State Depth** | Volume and complexity of local configuration |
| **Cloud Gap** | How much state is NOT restored via cloud sync |
| **User Pain** | Impact of losing this state |

**Inclusion Threshold:** Must score High on at least 2 of 3 criteria, with no Low scores.

### Tier 1: Recommended First 10

| # | App | Field | Primary Restore Value |
|---|-----|-------|----------------------|
| 1 | **Git** | Development | `.gitconfig`, aliases, credentials, hooks |
| 2 | **VS Code** | Development | Settings, extensions, snippets, workspaces |
| 3 | **PowerToys** | Power User | FancyZones layouts, Keyboard Manager remaps |
| 4 | **Chrome** | Browser | Extensions, settings, bookmarks |
| 5 | **Firefox** | Browser | `about:config`, containers, extensions |
| 6 | **Obsidian** | Productivity | Plugins, themes, vault settings |
| 7 | **Lightroom Classic** | Photography | Catalogs, presets, keywords |
| 8 | **OBS Studio** | Video/Streaming | Scenes, sources, profiles |
| 9 | **MSI Afterburner** | Hardware | OC profiles, fan curves |
| 10 | **Windows Terminal** | Development | Profiles, settings, themes |

### Full Tier 1 (24 Apps)

Development, Creative (Photography/Video/Audio), Browsers, Productivity, Power User, Hardware categories. See `docs/curation-matrix.md` for complete list.

### Tier 2 (21 Apps)

Deferred apps with similar value but lower priority. See `docs/curation-matrix.md`.

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `bootstrap` | Install endstate command to user PATH |
| `capture` | Capture current machine state into manifest |
| `plan` | Generate execution plan without applying |
| `apply` | Execute the plan (with optional `-DryRun`) |
| `restore` | Restore configuration files (requires `-EnableRestore`) |
| `export-config` | Export config files from system |
| `validate-export` | Validate export integrity |
| `revert` | Revert last restore operation |
| `verify` | Check current state against manifest |
| `doctor` | Diagnose environment issues |
| `report` | Show history of previous runs |
| `state` | Manage endstate state (reset, export, import) |

---

## Testing

```powershell
# Run all tests
.\scripts\test_pester.ps1

# Run unit tests only
.\scripts\test-unit.ps1

# Run specific suite
.\scripts\test_pester.ps1 -Path tests/unit

# Run with tag filter
.\scripts\test_pester.ps1 -Tag "Manifest"
```

---

## Conventions

- **Manifest format:** JSONC preferred (`.jsonc`), also supports `.json`, `.yaml`
- **Module naming:** `modules/apps/<app-id>/module.jsonc` where `<app-id>` is lowercase hyphenated
- **Test naming:** `<Subject>.Tests.ps1`
- **Engine scripts:** `<verb>.ps1`
- **Restore is opt-in:** Requires `-EnableRestore` flag
- **Backup before overwrite:** Files backed up to `state/backups/<timestamp>/`

---

## Key Invariants

1. **Idempotence** — Re-running converges to same result
2. **Non-destructive defaults** — No silent deletions
3. **Verification-first** — Success means desired state is observable
4. **Separation of concerns** — Install ≠ configure ≠ verify
5. **CLI is source of truth** — GUI is thin presentation layer

---

## Related Documentation

- `docs/catalog-layout.md` — Module/bundle/manifest structure
- `docs/curation-matrix.md` — Full Golden App assessment
- `docs/ai/PROJECT_SHADOW.md` — Architectural truth and invariants
- `docs/contracts/` — Integration contracts
