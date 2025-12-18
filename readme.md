# Provisioning

Machine provisioning and configuration management for Automation Suite.

---

## Provisioning Manifesto (v1)

### Purpose

Provisioning exists to reliably transform a machine from an unknown state into a known, verified desired state.

It installs software, restores configuration, applies system preferences, and verifies outcomes — safely, repeatably, and without guesswork.

### Core Principles

#### 1. Desired state over imperative steps

Provisioning describes *what should be true*, not a sequence of shell commands.

The system decides how to reach that state.

#### 2. Idempotence is mandatory

Re-running provisioning must:

- converge to the same result
- never duplicate work
- never corrupt an existing setup

Idempotence is a product feature, not a best-effort optimization.

#### 3. Install ≠ configure ≠ verify

These are separate concerns:

- **Drivers** install software
- **Restorers** apply configuration
- **Verifiers** prove correctness

No step silently assumes success.

#### 4. Verification is first-class

Every meaningful action must be verifiable.

"If it ran" is not success.
Success means the desired state is observable.

#### 5. Platform-agnostic by design

Provisioning is Windows-first in implementation, but platform-agnostic in architecture.

Manifests express intent, not OS-specific commands.
Drivers adapt intent to the platform.

#### 6. Safety before convenience

Defaults must be:

- non-destructive
- reversible where possible
- explicit when destructive

Existing state is backed up before modification.

#### 7. Deterministic planning

Before execution, Provisioning can:

- resolve drivers
- compute steps
- show exactly what will happen

No hidden work. No surprises.

#### 8. State is remembered

Provisioning records:

- what was intended
- what was applied
- what was skipped
- what failed, and why

This enables drift detection and confident re-runs.

#### 9. Human trust matters

Logs, plans, and reports are designed for humans, not just machines.

You should be able to read a run and understand it.

### Non-Goals (Explicit)

Provisioning is **not**:

- a remote fleet manager
- an always-on agent
- an enterprise MDM replacement
- a replacement for OS installers

It focuses on repeatable personal and small-team machines.

### The Prime Directive

> If Provisioning cannot be safely re-run at any time, it is incomplete.

---

## Architecture Overview

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
provisioning/
├── readme.md           # This file
├── cli.ps1             # CLI entrypoint (stub)
├── plans/              # Generated execution plans
├── engine/             # Core orchestration logic
├── drivers/            # Software installation adapters (winget, apt, brew)
├── restorers/          # Configuration restoration modules
├── verifiers/          # State verification modules
├── state/              # Persistent state (run history, checksums)
└── logs/               # Execution logs
```

| Directory | Purpose |
|-----------|---------|
| `plans/` | Stores generated execution plans before apply |
| `engine/` | Core planner, executor, and orchestration logic |
| `drivers/` | Platform-specific installers (e.g., `winget.ps1`, `apt.ps1`, `brew.ps1`) |
| `restorers/` | Config restoration modules (e.g., dotfiles, registry, symlinks) |
| `verifiers/` | Verification modules (e.g., file-exists, command-responds, hash-matches) |
| `state/` | Run history, applied manifests, checksums for drift detection |
| `logs/` | Human-readable execution logs per run |

---

## CLI

The CLI supports the following commands:

| Command | Description |
|---------|-------------|
| `capture` | Capture current machine state into a manifest |
| `plan` | Generate execution plan from manifest without applying |
| `apply` | Execute the plan (with optional `-DryRun`) |
| `verify` | Check current state against manifest without modifying |
| `doctor` | Diagnose environment issues (missing drivers, permissions, etc.) |
| `report` | Show history of previous runs and their outcomes |

**Example usage:**

```powershell
# Capture current machine state
.\cli.ps1 -Command capture -OutManifest .\manifests\my-machine.jsonc

# Generate and review plan
.\cli.ps1 -Command plan -Manifest .\manifests\my-machine.jsonc

# Apply with dry-run first
.\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc -DryRun

# Apply for real
.\cli.ps1 -Command apply -Manifest .\manifests\my-machine.jsonc

# Verify current state
.\cli.ps1 -Command verify -Manifest .\manifests\my-machine.jsonc

# Check environment health
.\cli.ps1 -Command doctor
```

---

## Manifest Format (v1)

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
    "./profiles/dev-tools.jsonc",
    "./apps/media.jsonc",
    "./configs/dotfiles.jsonc"
  ],

  // Local apps are merged with included apps
  "apps": [
    { "id": "custom-tool", "refs": { "windows": "Custom.Tool" } }
  ]
}
```

```jsonc
// profiles/dev-tools.jsonc
{
  "apps": [
    { "id": "git", "refs": { "windows": "Git.Git" } },
    { "id": "vscode", "refs": { "windows": "Microsoft.VisualStudioCode" } },
    { "id": "nodejs", "refs": { "windows": "OpenJS.NodeJS.LTS" } }
  ]
}
```

**Include rules:**
- Paths are resolved relative to the including manifest
- Arrays (`apps`, `restore`, `verify`) are concatenated
- Scalar fields in the root manifest take precedence
- Circular includes are detected and rejected with a clear error

### Key Concepts

| Field | Purpose |
|-------|--------|
| **`apps`** | Software to install, with platform-specific package refs |
| **`restore`** | Configuration to apply (copy files, symlinks) |
| **`verify`** | Verification steps beyond app-level checks |
| **`includes`** | Other manifest files to merge |

---

## Safety Defaults

Provisioning prioritizes safety over speed:

| Default | Behavior |
|---------|----------|
| **Backup before overwrite** | Existing files are backed up before restoration |
| **Non-destructive** | No deletions unless explicitly configured |
| **Dry-run support** | All commands support `--dry-run` to preview changes |
| **Explicit destructive ops** | Destructive operations require explicit flags |
| **Atomic operations** | Failed operations roll back where possible |
| **Checksum verification** | Restored files are verified against expected hashes |

**Backup location:** `state/backups/<timestamp>/`

---

## Status

**Current:** MVP functional — capture, plan, apply (with dry-run), and verify commands work.

See [roadmap.md](../roadmap.md) for planned development.
