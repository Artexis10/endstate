# Endstate

**Canonical home of the Endstate CLI** — A declarative system provisioning and recovery tool that restores a machine to a known-good end state safely, repeatably, and without guesswork.

**Author:** Hugo Ander Kivi  
**Primary Language:** PowerShell  
**Status:** Functional MVP — actively evolving

[![CI](https://github.com/Artexis10/endstate/actions/workflows/ci.yml/badge.svg)](https://github.com/Artexis10/endstate/actions/workflows/ci.yml)

> **Note:** This repository is the canonical home of the Endstate CLI. The CLI was previously part of the [automation-suite](https://github.com/Artexis10/automation-suite) repository but has been extracted into this standalone project for independent development and versioning.

---

## Why This Exists

Rebuilding a machine after a clean install is tedious, error-prone, and mentally draining. Configuration drift accumulates silently. Manual steps get forgotten. The result is machines that cannot be reliably reconstructed.

Endstate exists to eliminate this **clean install tax**.

A machine should be:

- **Rebuildable** — from a single manifest
- **Auditable** — with clear records of what was applied
- **Deterministic** — same inputs produce same outcomes
- **Safe to re-run** — at any time, without side effects

---

## Who This Is For

Endstate is designed for developers, power users, and small teams who:
- Reinstall or migrate machines regularly
- Care about reproducibility and auditability
- Want automation without sacrificing safety or control

---

## Core Principles

- **Declarative desired state** — describe *what should be true*, not *how to do it*
- **Idempotence** — re-running converges to the same result without duplicating work
- **Non-destructive defaults** — no silent deletions, explicit opt-in for destructive operations
- **Verification-first** — "it ran" is not success; success means the desired state is observable
- **Separation of concerns** — install ≠ configure ≠ verify

---

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

| Stage | Responsibility |
|-------|----------------|
| **Spec** | Declarative manifest describing desired state (apps, configs, preferences) |
| **Planner** | Resolves spec into executable steps, detects drift, computes minimal diff |
| **Drivers** | Install software via platform-specific package managers (winget, apt, brew) |
| **Restorers** | Apply configuration files, registry keys, symlinks, preferences |
| **Verifiers** | Confirm desired state is achieved (file exists, app responds, config matches) |
| **Reports/State** | Persist run history, enable drift detection, provide human-readable logs |

---

## Directory Structure

```
endstate/
├── bin/                # CLI entrypoints
│   ├── endstate.ps1    # Main CLI entrypoint
│   ├── endstate.cmd    # Windows CMD wrapper
│   └── cli.ps1         # Legacy provisioning subsystem CLI
├── engine/             # Core orchestration logic
├── drivers/            # Software installation adapters (winget, apt, brew)
├── restorers/          # Configuration restoration modules
├── verifiers/          # State verification modules
├── modules/            # Config module catalog (apps.git, apps.vscodium, etc.)
├── manifests/          # Desired state declarations
│   ├── examples/       # Shareable example manifests
│   ├── includes/       # Reusable manifest fragments
│   └── local/          # Machine-specific captures (gitignored)
├── tests/              # Pester unit tests
├── scripts/            # Test runners and utilities
└── tools/              # Vendored dependencies (Pester)
```

---

## Quickstart

### Initial Setup

```powershell
# Clone the repo
git clone https://github.com/ArtexisX/endstate.git
cd endstate

# (Optional) Unblock downloaded scripts
Get-ChildItem -Recurse -Filter *.ps1 | Unblock-File

# Bootstrap: Install endstate to PATH for global access
.\bin\endstate.ps1 bootstrap
```

After bootstrap completes, the `endstate` command is available globally from any directory.

### Basic Workflow

```powershell
# 1. Capture current machine state
endstate capture

# 2. Preview what would be applied (dry-run)
endstate apply -Manifest manifests/local/my-machine.jsonc -DryRun

# 3. Apply the manifest
endstate apply -Manifest manifests/local/my-machine.jsonc

# 4. Verify end state is achieved
endstate verify -Manifest manifests/local/my-machine.jsonc

# 5. Check environment health
endstate doctor
```

### CLI Commands

| Command | Description |
|---------|-------------|
| `bootstrap` | Install endstate command to user PATH for global access |
| `capture` | Capture current machine state into a manifest |
| `plan` | Generate execution plan from manifest without applying |
| `apply` | Execute the plan (with optional `-DryRun`) |
| `restore` | Restore configuration files from manifest (requires `-EnableRestore`) |
| `export-config` | Export config files from system to export folder (inverse of restore) |
| `validate-export` | Validate export integrity before restore |
| `revert` | Revert last restore operation by restoring backups |
| `verify` | Check current state against manifest without modifying |
| `doctor` | Diagnose environment issues (missing drivers, permissions, etc.) |
| `report` | Show history of previous runs and their outcomes |
| `state` | Manage endstate state (subcommands: reset, export, import) |

---

## Manifest Format

**Humans author manifests in JSONC** (JSON with comments). Plans, state, and reports are emitted as plain JSON.

Supported formats: `.jsonc` (preferred), `.json`, `.yaml`, `.yml`

### Basic Example

```jsonc
// my-machine.jsonc
{
  "version": 1,
  "name": "dev-workstation",

  // Applications to install
  "apps": [
    {
      "id": "vscode",
      "refs": {
        "windows": "Microsoft.VisualStudioCode",
        "linux": "code",
        "macos": "visual-studio-code"
      }
    },
    {
      "id": "git",
      "refs": {
        "windows": "Git.Git",
        "linux": "git",
        "macos": "git"
      }
    }
  ],

  // Configuration restore (opt-in)
  "restore": [
    { "type": "copy", "source": "./configs/.gitconfig", "target": "~/.gitconfig", "backup": true }
  ],

  // Verification steps
  "verify": [
    { "type": "file-exists", "path": "~/.gitconfig" }
  ]
}
```

### Modular Manifests with Includes

Large manifests can be split into reusable modules:

```jsonc
// main.jsonc
{
  "version": 1,
  "name": "dev-workstation",
  
  // Include other manifest files (resolved relative to this file)
  "includes": [
    "./includes/dev-tools.jsonc",
    "./includes/media.jsonc",
    "./includes/dotfiles.jsonc"
  ],

  // Local apps are merged with included apps
  "apps": [
    { "id": "custom-tool", "refs": { "windows": "Custom.Tool" } }
  ]
}
```

**Include rules:**
- Paths are resolved relative to the including manifest
- Arrays (`apps`, `restore`, `verify`) are concatenated
- Scalar fields in the root manifest take precedence
- Circular includes are detected and rejected with a clear error

---

## Safety Defaults

Endstate prioritizes safety over speed:

| Default | Behavior |
|---------|----------|
| **Backup before overwrite** | Existing files are backed up before restoration |
| **Non-destructive** | No deletions unless explicitly configured |
| **Dry-run support** | All commands support `-DryRun` to preview changes |
| **Explicit destructive ops** | Destructive operations require explicit flags |
| **Atomic operations** | Failed operations roll back where possible |
| **Checksum verification** | Restored files are verified against expected hashes |

**Backup location:** `state/backups/<timestamp>/`

---

## Prerequisites

| Requirement | Version | Purpose |
|-------------|---------|---------|
| PowerShell | 5.1+ | Script execution |
| winget | Latest | App installation (Windows) |

### Optional Dependencies

- **ffmpeg / ffprobe** — Media conversion utilities
- **apt / brew** — Package managers for Linux/macOS (future support)

---

## Testing

Endstate uses Pester 5.7.1 (vendored in `tools/pester/`) for deterministic, offline-capable testing.

```powershell
# Run all tests
.\scripts\test_pester.ps1

# Run specific test suite
.\scripts\test_pester.ps1 -Path tests/unit

# Run tests with tag filter
.\scripts\test_pester.ps1 -Tag "Manifest"
```

---

## Status

**Current:** MVP functional — capture, apply, verify, and drift detection work. Restore operations are opt-in. Custom drivers are supported but winget is the primary driver.

**Maturity:** This is a personal/small-team tool. It is not enterprise software. It prioritizes correctness and safety over features.

> A desktop GUI is planned as a separate commercial product built on top of Endstate's open-source core.

---

## History

Endstate was originally developed as the `provisioning/` subsystem within the [automation-suite](https://github.com/Artexis10/automation-suite) repository. It has been split into a standalone project to:

- Focus development on machine provisioning as a first-class product
- Enable independent versioning and releases
- Simplify contribution and adoption
- Maintain a clean separation of concerns

The full git history has been preserved in this repository.

---

## License

Endstate is licensed under the Apache License, Version 2.0.

See the [LICENSE](LICENSE) file for details.

Copyright © 2025 Substrate Systems OÜ

Created by Hugo Ander Kivi at Substrate Systems OÜ.

