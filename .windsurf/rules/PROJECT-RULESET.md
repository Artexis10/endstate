---
trigger: always
---

# Autosuite Project Ruleset

This ruleset governs development and operation of the Autosuite repository.

---

## Repository Identity

**Autosuite** is a standalone machine provisioning and configuration management tool.

- **Repository**: autosuite (standalone, not part of automation-suite umbrella)
- **Entrypoint**: `cli.ps1`
- **Primary Language**: PowerShell
- **Status**: Functional MVP — actively evolving

---

## Glossary

| Term | Definition |
|------|------------|
| **Manifest** | Declarative JSONC/JSON/YAML file describing desired machine state (apps, configs, verifications) |
| **Plan** | Generated execution steps derived from a manifest, showing exactly what will happen |
| **State** | Persistent record of previous runs, applied manifests, and checksums for drift detection |
| **Driver** | Platform-specific adapter for installing software (e.g., winget, apt, brew) |
| **Restorer** | Module that applies configuration (copy files, merge JSON/INI, append lines) |
| **Verifier** | Module that confirms desired state is achieved (file exists, command responds, hash matches) |
| **Report** | JSON artifact summarizing a run: what was intended, applied, skipped, and failed |

---

## Repository Structure

```
autosuite/
├── cli.ps1             # CLI entrypoint
├── engine/             # Core orchestration logic
│   ├── manifest.ps1    # Manifest parsing (JSONC/JSON/YAML)
│   ├── plan.ps1        # Plan generation
│   ├── apply.ps1       # Execution engine
│   ├── capture.ps1     # State capture (winget export)
│   ├── verify.ps1      # State verification
│   ├── restore.ps1     # Config restoration (opt-in)
│   ├── state.ps1       # State persistence
│   ├── report.ps1      # Run history and reporting
│   ├── diff.ps1        # Artifact comparison
│   └── logging.ps1     # Logging utilities
├── drivers/            # Software installation adapters
│   └── winget.ps1      # Windows Package Manager driver
├── restorers/          # Configuration restoration modules
│   ├── copy.ps1        # File/directory copy
│   ├── merge-json.ps1  # JSON deep merge
│   ├── merge-ini.ps1   # INI section/key merge
│   └── append.ps1      # Append lines to files
├── verifiers/          # State verification modules
│   └── file-exists.ps1 # File existence check
├── modules/            # Config module catalog
│   └── apps/           # App-specific config modules (module.jsonc)
├── manifests/          # Desired state declarations
│   ├── examples/       # Shareable example manifests (committed)
│   ├── includes/       # Reusable manifest fragments (committed)
│   └── local/          # Machine-specific captures (gitignored)
├── plans/              # Generated execution plans (gitignored)
├── state/              # Run history and checksums (gitignored)
├── logs/               # Execution logs (gitignored)
├── tests/              # Pester unit tests
│   ├── unit/           # Unit tests
│   └── fixtures/       # Test fixtures
├── scripts/            # Test runners and utilities
│   ├── test_pester.ps1 # Main test runner
│   └── ensure-pester.ps1 # Vendored Pester loader
└── tools/              # Vendored dependencies
    └── pester/         # Pester 5.7.1 (committed)
```

---

## Design Principles

### 1. Platform-Agnostic
Manifests express intent, not OS-specific commands. Drivers adapt intent to the platform.

### 2. Idempotent by Default
Re-running any operation must:
- Converge to the same result
- Never duplicate work
- Never corrupt existing state
- Log what was skipped and why

### 3. Non-Destructive + Safe
- Defaults preserve original data
- Backups created before overwrites
- Destructive operations require explicit opt-in flags
- No secrets auto-exported (sensitive paths excluded from capture)

### 4. Declarative Desired State
Describe *what should be true*, not imperative steps. The system decides how to reach that state.

### 5. Separation of Concerns
- **Drivers** install software
- **Restorers** apply configuration
- **Verifiers** prove correctness
- No step silently assumes success

### 6. First-Class Verification
Every meaningful action must be verifiable. "It ran" is not success—success means the desired state is observable.

### 7. Deterministic Output
- Manifest hash: 16-character SHA256 prefix
- RunId format: `yyyyMMdd-HHmmss`
- JSON reports: ordered keys (runId, timestamp, manifest, summary, actions)
- Plans reproducible given same manifest + installed state
- Capture output sorted alphabetically by app id

---

## How to Run

Autosuite supports **two invocation styles**:

### 1. Direct Invocation (from repo directory)
```powershell
.\cli.ps1 -Command <command> [options]
```

### 2. PATH-Installed Invocation (from anywhere)
```powershell
autosuite <command> [options]
```

Both styles support identical commands and flags.

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `capture` | Capture current machine state into a manifest |
| `plan` | Generate execution plan from manifest without applying |
| `apply` | Execute the plan (with optional `-DryRun`) |
| `verify` | Check current state against manifest without modifying |
| `restore` | Restore configuration files from manifest (requires `-EnableRestore`) |
| `doctor` | Diagnose environment issues (missing drivers, permissions, etc.) |
| `report` | Show history of previous runs and their outcomes |
| `diff` | Compare two plan/run artifacts |

---

## Canonical Workflow

```powershell
# 1. CAPTURE: Export current machine state
.\cli.ps1 -Command capture -Profile my-machine
# or: autosuite capture -Profile my-machine

# 2. PLAN: Preview what would be applied
.\cli.ps1 -Command plan -Manifest manifests/my-machine.jsonc
# or: autosuite plan -Manifest manifests/my-machine.jsonc

# 3. APPLY: Execute the plan (use -DryRun first!)
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc -DryRun
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc
# or: autosuite apply -Manifest manifests/my-machine.jsonc

# 4. VERIFY: Confirm desired state is achieved
.\cli.ps1 -Command verify -Manifest manifests/my-machine.jsonc
# or: autosuite verify -Manifest manifests/my-machine.jsonc

# 5. DOCTOR: Check environment health
.\cli.ps1 -Command doctor
# or: autosuite doctor
```

**Note**: Restore is **opt-in** and requires explicit `-EnableRestore` flag.

---

## Command Reference

### Capture

```powershell
# Capture with profile name (recommended)
.\cli.ps1 -Command capture -Profile my-machine
autosuite capture -Profile my-machine

# Capture to explicit path
.\cli.ps1 -Command capture -OutManifest manifests/my-machine.jsonc
autosuite capture -OutManifest manifests/my-machine.jsonc

# Capture with templates
.\cli.ps1 -Command capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate
autosuite capture -Profile my-machine -IncludeRestoreTemplate -IncludeVerifyTemplate

# Capture with runtimes and store apps
.\cli.ps1 -Command capture -Profile my-machine -IncludeRuntimes -IncludeStoreApps
autosuite capture -Profile my-machine -IncludeRuntimes -IncludeStoreApps

# Capture minimized (drop entries without stable refs)
.\cli.ps1 -Command capture -Profile my-machine -Minimize
autosuite capture -Profile my-machine -Minimize

# Capture with discovery mode
.\cli.ps1 -Command capture -Profile my-machine -Discover
autosuite capture -Profile my-machine -Discover

# Update existing manifest (merge new apps)
.\cli.ps1 -Command capture -Profile my-machine -Update
autosuite capture -Profile my-machine -Update

# Update with pruning (remove apps no longer installed)
.\cli.ps1 -Command capture -Profile my-machine -Update -PruneMissingApps
autosuite capture -Profile my-machine -Update -PruneMissingApps

# Capture with config files from matched modules
.\cli.ps1 -Command capture -Profile my-machine -WithConfig
autosuite capture -Profile my-machine -WithConfig

# Capture from specific modules
.\cli.ps1 -Command capture -Profile my-machine -WithConfig -ConfigModules apps.git,apps.vscodium
autosuite capture -Profile my-machine -WithConfig -ConfigModules apps.git,apps.vscodium

# Capture config to custom payload directory
.\cli.ps1 -Command capture -Profile my-machine -WithConfig -PayloadOut .\my-payload
autosuite capture -Profile my-machine -WithConfig -PayloadOut .\my-payload
```

### Plan

```powershell
# Generate execution plan
.\cli.ps1 -Command plan -Manifest manifests/my-machine.jsonc
autosuite plan -Manifest manifests/my-machine.jsonc
```

### Apply

```powershell
# Apply with dry-run preview
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc -DryRun
autosuite apply -Manifest manifests/my-machine.jsonc -DryRun

# Apply for real
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc
autosuite apply -Manifest manifests/my-machine.jsonc

# Apply from pre-generated plan
.\cli.ps1 -Command apply -Plan plans/20251219-010000.json
autosuite apply -Plan plans/20251219-010000.json

# Apply with restore enabled (opt-in)
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc -EnableRestore
autosuite apply -Manifest manifests/my-machine.jsonc -EnableRestore
```

### Verify

```powershell
# Verify current state matches manifest
.\cli.ps1 -Command verify -Manifest manifests/my-machine.jsonc
autosuite verify -Manifest manifests/my-machine.jsonc
```

### Restore

```powershell
# Restore configuration files (requires explicit opt-in)
.\cli.ps1 -Command restore -Manifest manifests/my-machine.jsonc -EnableRestore
autosuite restore -Manifest manifests/my-machine.jsonc -EnableRestore

# Restore with dry-run preview
.\cli.ps1 -Command restore -Manifest manifests/my-machine.jsonc -EnableRestore -DryRun
autosuite restore -Manifest manifests/my-machine.jsonc -EnableRestore -DryRun
```

### Report

```powershell
# Show most recent run (default)
.\cli.ps1 -Command report
autosuite report

# Show most recent run (explicit)
.\cli.ps1 -Command report -Latest
autosuite report -Latest

# Show specific run by ID
.\cli.ps1 -Command report -RunId 20251219-013701
autosuite report -RunId 20251219-013701

# Show last 5 runs (compact list)
.\cli.ps1 -Command report -Last 5
autosuite report -Last 5

# Output report as JSON
.\cli.ps1 -Command report -Json
autosuite report -Json
```

### Diff

```powershell
# Compare two plan/run artifacts
.\cli.ps1 -Command diff -FileA plans/run1.json -FileB plans/run2.json
autosuite diff -FileA plans/run1.json -FileB plans/run2.json

# Diff with JSON output
.\cli.ps1 -Command diff -FileA plans/run1.json -FileB plans/run2.json -Json
autosuite diff -FileA plans/run1.json -FileB plans/run2.json -Json
```

### Doctor

```powershell
# Diagnose environment issues
.\cli.ps1 -Command doctor
autosuite doctor
```

---

## Command Options

### Capture Options

| Option | Default | Description |
|--------|---------|-------------|
| `-Profile <name>` | - | Profile name; writes to `manifests/<name>.jsonc` |
| `-OutManifest <path>` | - | Explicit output path (overrides -Profile) |
| `-IncludeRuntimes` | false | Include runtime packages (VCRedist, .NET, UI.Xaml, etc.) |
| `-IncludeStoreApps` | false | Include Microsoft Store apps (msstore source or 9N*/XP* IDs) |
| `-Minimize` | false | Drop entries without stable refs (no windows ref) |
| `-IncludeRestoreTemplate` | false | Generate `./includes/<profile>-restore.jsonc` (requires -Profile) |
| `-IncludeVerifyTemplate` | false | Generate `./includes/<profile>-verify.jsonc` (requires -Profile) |
| `-Discover` | false | Enable discovery mode: detect software present but not winget-managed |
| `-DiscoverWriteManualInclude` | true (when -Discover) | Generate `./includes/<profile>-manual.jsonc` with commented suggestions |
| `-Update` | false | Merge new capture into existing manifest instead of overwriting |
| `-PruneMissingApps` | false | With -Update, remove apps no longer present (root manifest only) |
| `-WithConfig` | false | Capture config files from matched config modules into payload directory |
| `-ConfigModules <list>` | - | Explicitly specify which config modules to capture (comma-separated) |
| `-PayloadOut <path>` | `payload/` | Output directory for captured config payloads |

### Apply Options

| Option | Default | Description |
|--------|---------|-------------|
| `-Manifest <path>` | - | Path to manifest file (JSONC/JSON/YAML) |
| `-Plan <path>` | - | Path to pre-generated plan file (mutually exclusive with -Manifest) |
| `-DryRun` | false | Preview changes without applying |
| `-EnableRestore` | false | Enable restore operations (opt-in for safety) |

### Report Options

| Option | Default | Description |
|--------|---------|-------------|
| `-Latest` | true | Show most recent run (default behavior) |
| `-RunId <id>` | - | Show specific run by ID (mutually exclusive with -Latest/-Last) |
| `-Last <n>` | - | Show N most recent runs in compact list format |
| `-Json` | false | Output as machine-readable JSON |

---

## JSONC Manifest Support

**All manifest and plan parsing supports JSONC (JSON with Comments).**

**Critical**: The JSONC parser is fully compatible with **Windows PowerShell 5.1** (stock Win11) and **PowerShell 7+**.

### Canonical JSONC Loader

The engine uses a single canonical function for all JSONC parsing:

- **Function**: `Read-JsoncFile` in `engine/manifest.ps1`
- **Purpose**: Parse JSONC files with comment stripping (single-line `//` and multi-line `/* */`)
- **Implementation**: PS5.1-safe state machine that preserves strings containing `//` (e.g., `"http://example.com"`)
- **Depth**: Default 100 levels for deeply nested structures
- **Compatibility**: Automatically detects PS version and uses appropriate parsing strategy

### Comment Support

JSONC files support:
- **Single-line comments**: `// comment text`
- **Multi-line comments**: `/* comment text */`
- **Inline comments**: `"key": "value" // inline comment`

Comments are stripped **only when outside JSON strings**, preserving URLs and other string content containing `//` or `/*`.

### Implementation Details

**Do NOT use `ConvertFrom-Json` directly for manifests/plans/state files. Always use `Read-JsoncFile`.**

All code paths that parse manifests, plans, or state files use `Read-JsoncFile`:
- Manifest loading (`Read-Manifest`, `Read-ManifestRaw`)
- Plan file loading (`Invoke-ApplyFromPlan`)
- State file loading (`Read-StateFile`)
- Artifact file loading (`Read-ArtifactFile`)

---

## Manifest Format (v1)

Supported formats: `.jsonc` (preferred), `.json`, `.yaml`, `.yml`

```jsonc
{
  "version": 1,
  "name": "my-workstation",
  "captured": "2025-01-01T00:00:00Z",
  
  "includes": [
    "./includes/my-workstation-restore.jsonc",
    "./includes/my-workstation-verify.jsonc"
  ],
  
  "apps": [
    {
      "id": "vscode",
      "refs": {
        "windows": "Microsoft.VisualStudioCode",
        "linux": "code",
        "macos": "visual-studio-code"
      }
    }
  ],
  
  "restore": [
    { "type": "copy", "source": "./configs/.gitconfig", "target": "~/.gitconfig", "backup": true }
  ],
  
  "verify": [
    { "type": "file-exists", "path": "~/.gitconfig" }
  ]
}
```

---

## Config Modules (v1.1)

Config modules provide reusable restore/verify/capture configurations for applications.

**Location:** `modules/apps/<app>/module.jsonc`

**Manifest Field:**
```jsonc
{
  "configModules": ["apps.git", "apps.vscodium"]
}
```

**Module Schema:**
```jsonc
{
  "id": "apps.git",
  "displayName": "Git",
  "sensitivity": "low",
  "matches": {
    "winget": ["Git.Git"],
    "exe": ["git.exe"],
    "uninstallDisplayName": ["^Git\\b"]
  },
  "verify": [
    { "type": "command-exists", "command": "git" },
    { "type": "file-exists", "path": "~/.gitconfig" }
  ],
  "restore": [
    { "type": "copy", "source": "./payload/apps/git/.gitconfig", "target": "~/.gitconfig", "backup": true }
  ],
  "capture": {
    "files": [
      { "source": "~/.gitconfig", "dest": "apps/git/.gitconfig", "optional": true }
    ]
  }
}
```

---

## Engineering Discipline

### Idempotency Requirements
- Every operation must detect current state before acting
- Skip actions when desired state already exists
- Log skipped actions with reason: `[SKIP] <item> - already installed`
- Drift detection: compare current state hash against last-run state

### Backup Policy
- Restorers must backup existing files before overwriting
- Backup location: `state/backups/<runId>/`
- Backup format: original path structure preserved

### Security
- No secrets auto-exported during capture
- Sensitive paths excluded: `.ssh`, `.aws`, `.azure`, `Credentials`
- API keys never hardcoded; use environment variables
- Warn user when sensitive paths detected

### Testing Requirements
- Mock all external calls (winget, network) in tests
- No real installs in CI/tests
- Tests must be deterministic and idempotent
- Use fixtures in `tests/fixtures/`

### Deterministic Output
- Manifest hash: 16-character SHA256 prefix
- RunId format: `yyyyMMdd-HHmmss`
- JSON reports: ordered keys (runId, timestamp, manifest, summary, actions)
- Plans reproducible given same manifest + installed state
- Capture apps sorted alphabetically by app id

### Logging + Reports
- All runs produce logs in `logs/<runId>.log`
- Reports saved as JSON in `state/runs/<runId>.json`
- Report schema: `runId`, `timestamp`, `manifest`, `summary`, `actions`
- Human-readable console output with color coding

---

## Hard Rules — Git Commit Policy

### NEVER Commit Runtime Artifacts

The following directories contain runtime-generated data and **MUST NEVER** be committed:

- `logs/` — execution logs (gitignored)
- `plans/` — generated execution plans (gitignored)
- `state/` — run history and checksums (gitignored)

**Enforcement**: These directories are gitignored. If you see them in `git status`, **DO NOT** commit them.

### NEVER Commit Local Test/Smoke Manifests

The following manifest patterns are for local testing only and **MUST NEVER** be committed:

- `manifests/local/` — machine-specific captures (gitignored)
- `manifests/*-smoke*.jsonc` — smoke test manifests (gitignored)
- `manifests/test-*.jsonc` — test manifests (gitignored)

**Enforcement**: These patterns are gitignored. If you see them in `git status`, **DO NOT** commit them.

### Committed Manifests

Only the following manifests should be committed:

- `manifests/examples/` — sanitized, shareable example manifests
- `manifests/includes/` — reusable manifest fragments
- Test fixtures in `tests/fixtures/` (deterministic, non-machine-specific)

---

## Documentation Drift Rule

**If CLI commands, flags, or manifest schema change, you MUST update both README.md and this ruleset in the SAME commit.**

This ensures documentation stays synchronized with implementation.

### What Triggers This Rule

Changes to any of the following require README + ruleset updates:

- CLI commands (add/remove/rename)
- CLI parameters or flags (add/remove/rename)
- Manifest schema fields (add/remove/rename)
- Default values for flags or options
- Environment variables
- Directory structure
- New drivers/restorers/verifiers

### Enforcement

- Code reviewers must verify README and ruleset are updated when CLI/schema changes
- CI does not enforce this (manual review required)
- If you forget, amend the commit or create a follow-up commit immediately

---

## Change Management

### Destructive Operations
- Restore is opt-in: requires `-EnableRestore` flag
- Backups stored in `state/backups/<runId>/` preserving path structure
- Sensitive paths (.ssh, .aws, credentials, etc.) trigger warnings
- Must be explicitly opt-in (require flags like `-Force` or `-Confirm`)
- Must log clearly: `[DESTRUCTIVE] <action>`
- Must backup before proceeding

### Reboot Markers
- Operations requiring reboot must set `requiresReboot: true` in plan/report
- CLI must warn user at end of run if reboot required
- Planned: `--reboot-if-needed` flag (not implemented yet)

---

## Documentation Naming Conventions

### Canonical Documents (UPPERCASE)

Project-level, canonical entry-point documents MUST use uppercase filenames:

- README.md
- VISION.md
- CONTRIBUTING.md
- LICENSE
- SECURITY.md
- CHANGELOG.md

### Supporting Documentation (lowercase)

All non-canonical documentation MUST use lowercase filenames, including:

- docs/**/*.md
- tool-specific documentation
- conceptual, lifecycle, or reference materials

### General Rules

- Do NOT mix casing styles (e.g. Readme.md, Vision.md)
- Do NOT rename files back and forth purely for casing
- Consistency takes precedence over personal preference

---

## Vendored Pester Policy

This repo values hermetic, deterministic, offline-capable tooling:

- **Pester 5.7.1 is vendored** in `tools/pester/` and committed to the repository
- Tests always use vendored Pester first, never global modules
- `scripts/ensure-pester.ps1` prepends `tools/pester/` to `$env:PSModulePath`
- If vendored Pester is missing, it bootstraps via: `Save-Module Pester -Path tools/pester -RequiredVersion 5.7.1`

---

## Running Tests

```powershell
# From repo root - run all Pester tests (recommended)
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts\test_pester.ps1

# Run specific test suite
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts\test_pester.ps1 -Path tests\unit

# Run specific test file
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts\test_pester.ps1 -Path tests\unit\Manifest.Tests.ps1
```

**Exit codes**: The test runner exits 0 on success, non-zero on failure.

---

## Continuous Integration (CI)

GitHub Actions runs hermetic unit tests on:
- Pull requests targeting `main`
- Pushes to `main`

**Docs-only changes do NOT trigger CI** (via `paths-ignore` for `**/*.md` and `docs/**`).

### CI Workflow

Location: `.github/workflows/ci.yml`

### CI Command

```powershell
pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/test_pester.ps1 -Path tests/unit
```

### CI Principles

- **Hermetic**: No real winget installs; all external calls mocked
- **Windows-first**: `runs-on: windows-latest`
- **Cost-controlled**: `paths-ignore` prevents docs-only runs
- **Vendored Pester**: Uses committed `tools/pester/` for deterministic execution

---

## Not Yet Implemented

The following are planned but not yet functional:

- **apt/dnf/brew drivers** — Linux/macOS package managers
- **Verifier modules** — Custom verification beyond file-exists
- **Reboot handling** — Automatic reboot detection and `--reboot-if-needed`
- **Rollback** — Undo last apply using backup state

---

## References

- [README.md](../README.md) - Project overview and quickstart
- [VISION.md](../VISION.md) - Design philosophy and goals
- [WARP.md](../WARP.md) - Development roadmap and milestones
