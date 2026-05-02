# Endstate

**Canonical home of the Endstate CLI** — A declarative system provisioning and recovery tool that restores a machine to a known-good end state safely, repeatably, and without guesswork.

**Author:** Hugo Ander Kivi  
**Primary Language:** Go
**Status:** v1.0.0 — Stable

[![CI](https://github.com/Artexis10/endstate/actions/workflows/ci.yml/badge.svg)](https://github.com/Artexis10/endstate/actions/workflows/ci.yml)

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
├── go-engine/          # Go engine (sole CLI implementation)
│   ├── cmd/endstate/   # CLI entrypoint
│   └── internal/       # Core packages (manifest, commands, driver, restore, verifier, etc.)
├── modules/            # Config module catalog (apps.git, apps.vscodium, etc.)
├── payload/            # Staged configuration files referenced by modules
├── bundles/            # Named module groupings (JSONC)
├── manifests/          # Desired state declarations
│   ├── examples/       # Shareable example manifests
│   ├── includes/       # Reusable manifest fragments
│   └── local/          # Machine-specific captures (gitignored)
└── tests/              # Test fixtures and shared test data
```

---

## Quickstart

### Initial Setup

```bash
# Clone the repo
git clone https://github.com/Artexis10/endstate.git
cd endstate

# Build the CLI
cd go-engine && go build -o endstate.exe ./cmd/endstate

# Or run directly without building
cd go-engine && go run ./cmd/endstate bootstrap
```

After bootstrap completes, the `endstate` command is available globally from any directory.

### Basic Workflow

```bash
# 1. Capture current machine state
endstate capture

# 2. Preview what would be applied (dry-run)
endstate apply --manifest manifests/local/my-machine.jsonc --dry-run

# 3. Apply the manifest
endstate apply --manifest manifests/local/my-machine.jsonc

# 4. Verify end state is achieved
endstate verify --manifest manifests/local/my-machine.jsonc

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

## Hosted Backup (optional, paid tier)

End-to-end encrypted profile backups via Endstate Cloud (or any self-host
backend implementing `docs/contracts/hosted-backup-contract.md`). The
engine authenticates against the backend's OIDC endpoints, persists the
refresh token in Windows Credential Manager, and orchestrates chunked
upload/download against R2 presigned URLs.

The cryptographic primitives (Argon2id KDF, AES-256-GCM, BIP39 recovery
key) are isolated in a follow-up engine release for focused security
review. Until that release, `backup login`, `backup push`, `backup pull`,
and `backup recover` surface a clear `INTERNAL_ERROR` "crypto module not
yet implemented" message — the orchestration is wired and tested but the
key derivation cannot complete.

### Configuration

| Variable | Default | Purpose |
|---|---|---|
| `ENDSTATE_OIDC_ISSUER_URL` | `https://substratesystems.io` | OIDC issuer / backend URL |
| `ENDSTATE_OIDC_AUDIENCE` | `endstate-backup` | JWT audience claim |
| `ENDSTATE_BACKUP_CONCURRENCY` | `4` | Upload/download worker pool size (clamped 1–16) |

### Commands

```bash
# Sign in (passphrase via stdin — never as a flag)
endstate backup login --email you@example.com

# Report current session state
endstate backup status --json

# Sign out (clears local keychain entry; backend logout is best-effort)
endstate backup logout

# Inventory
endstate backup list --json
endstate backup versions --backup-id <id> --json

# Push and pull profile snapshots
endstate backup push --profile <path> [--name <label>]
endstate backup pull --backup-id <id> [--version-id <id>] --to <path>

# Destructive operations require --confirm
endstate backup delete --backup-id <id> --confirm
endstate backup delete-version --backup-id <id> --version-id <id> --confirm

# Forgotten passphrase (recovery key + new passphrase via stdin)
endstate backup recover --email you@example.com

# GDPR account deletion (destroys backups + subscription)
endstate account delete --confirm
```

The capabilities response (`endstate capabilities --json`) advertises the
configured issuer and audience under `data.features.hostedBackup`, so the
GUI can gate hosted-backup UI on a single handshake.

---

## Prerequisites

| Requirement | Version | Purpose |
|-------------|---------|---------|
| Go | 1.22+ | Build and development |
| winget | Latest | App installation (Windows) |

---

## Testing

Endstate uses Go's standard `testing` package for deterministic, offline-capable testing.

```bash
# Run all tests
cd go-engine && go test ./...

# Run a specific package's tests
cd go-engine && go test ./internal/manifest/...

# Run tests with verbose output
cd go-engine && go test -v ./...
```

---

## Status

Endstate v1.0.0 is the first stable release. The CLI contract (JSON schema 1.0) is locked. Capture, apply, verify, restore, and drift detection are production-ready. Winget is the primary driver with planned expansion to macOS/Linux.

> A desktop GUI is available as a separate commercial product built on top of Endstate's open-source core — [substratesystems.io/endstate](https://substratesystems.io/endstate)

---

## License

Endstate is licensed under the Apache License, Version 2.0.

See the [LICENSE](LICENSE) file for details.

Copyright © 2025–2026 Substrate Systems OÜ

Created by Hugo Ander Kivi at Substrate Systems OÜ.

