---
trigger: always
---

# Endstate Project Ruleset

This document is the **authoritative source of truth** for the Endstate CLI and development conventions.

**Scope:** CLI ↔ GUI contract (canonical), repository structure, engineering discipline, and operational guidelines.

---

## Repository Identity

**Endstate** is a standalone machine provisioning and configuration management tool.

- **Repository**: Endstate (standalone, not part of automation-suite umbrella)
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
Endstate/
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

## CLI ↔ GUI Contract

### Core Principles

1. **Thin GUI:** Endstate GUI contains no business logic, no provisioning logic, and makes no assumptions about internal CLI implementation.

2. **CLI as Source of Truth:** All operations are executed by CLI invocation. GUI is purely a presentation layer.

3. **Explicit Versioning:** Both CLI version and JSON schema version are explicit and machine-readable.

4. **Graceful Degradation:** Unknown fields in JSON responses are ignored by the GUI.

---

## Capabilities Handshake Requirement

Before executing any CLI command, the GUI **must** perform a capabilities handshake:

```
Endstate capabilities --json
```

### Handshake Flow

1. GUI calls `Endstate capabilities --json`
2. GUI parses the JSON envelope
3. GUI validates `schemaVersion` is within supported range
4. If incompatible: GUI shows clear error and refuses execution
5. If compatible: GUI proceeds with CLI invocation

### Required Validation

- `schemaVersion` must be checked against GUI's supported range
- GUI must refuse execution if schema version is incompatible
- GUI must cache capabilities for the session

---

## Schema Compatibility Enforcement

### Versioning Rules

- **CLI Version:** Follows Semantic Versioning (MAJOR.MINOR.PATCH)
- **Schema Version:** Uses MAJOR.MINOR format

### Compatibility Rules

| Change Type | Schema Version | CLI Version |
|-------------|----------------|-------------|
| Additive (new optional fields) | No change | MINOR bump |
| Breaking (removed/changed fields) | MAJOR bump | MAJOR bump |

### GUI Behavior

- GUI must validate schema version before any command execution
- GUI must display clear error message for incompatible versions
- GUI must not attempt to parse incompatible responses

---

## Execution Model

### Development Mode

During development, Endstate GUI resolves the CLI from the system PATH:

- **CLI Resolution:** `Endstate` command resolved from PATH
- **Execution:** Node.js `child_process.spawn`
- **Validation:** Capabilities handshake on startup

### Production Mode (Model B)

Production builds of Endstate GUI bundle a pinned Endstate CLI binary:

- **CLI Resolution:** Bundled binary at known path
- **Execution:** Tauri/Rust Command API
- **Validation:** Capabilities handshake on startup

### Execution Boundary

The `cli-bridge.ts` module defines the platform-agnostic contract:
- Types and interfaces for JSON responses
- Schema validation functions
- Abstract execution boundary

Platform-specific implementation is provided by the runtime layer:
- Development: Node.js child_process
- Production: Tauri/Rust backend

---

## JSON Contract v1.0

### Standard Envelope

Every `--json` output includes this envelope:

```json
{
  "schemaVersion": "1.0",
  "cliVersion": "0.1.0",
  "command": "apply",
  "runId": "20241220-143052",
  "timestampUtc": "2024-12-20T14:30:52Z",
  "success": true,
  "data": { ... },
  "error": null
}
```

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `schemaVersion` | string | JSON schema version |
| `cliVersion` | string | CLI version (semver) |
| `command` | string | Command that produced output |
| `runId` | string | Unique run ID (yyyyMMdd-HHmmss) |
| `timestampUtc` | string | ISO 8601 UTC timestamp |
| `success` | boolean | Whether command succeeded |
| `data` | object | Command-specific result |
| `error` | object/null | Error object if failed |

### Error Object

```json
{
  "code": "MANIFEST_NOT_FOUND",
  "message": "The specified manifest file does not exist.",
  "detail": { "path": "C:\\manifests\\missing.jsonc" },
  "remediation": "Check the file path and ensure the manifest exists.",
  "docsKey": "errors/manifest-not-found"
}
```

---

## Supported Commands (JSON Output)

Commands that support `--json` flag for GUI integration:

| Command | JSON Flag | Description |
|---------|-----------|-------------|
| `capabilities` | `--json` / `-Json` | Report CLI capabilities for handshake |
| `apply` | `--json` / `-Json` | Execute provisioning plan |
| `verify` | `--json` / `-Json` | Verify machine state against manifest |
| `report` | `--json` / `-Json` | Retrieve run history |

**GNU-style Flag Support:** All commands support both PowerShell-style flags (`-Json`, `-Profile`, `-Manifest`, `-Out`) and GNU-style double-dash flags (`--json`, `--profile`, `--manifest`, `--out`) for broader CLI compatibility.

**JSON Mode Behavior:**
- When `--json` or `-Json` is specified, the CLI emits a **JSON envelope as a single-line compressed JSON on the last line of stdout**.
- Logs, banners, and progress messages may appear on stdout before the JSON envelope.
- The JSON envelope is always the final payload and is a single line of valid JSON (using `ConvertTo-Json -Compress`).
- On failure, the JSON envelope contains `success: false`, populated `error` object, and non-zero exit code.
- GUIs should scan stdout from bottom to top to find the first line starting with `{` that parses as valid JSON with `schemaVersion`, `command`, and `success` fields.
- If no envelope is found but exit code is 0, GUIs should treat as success with a warning.

## Streaming Progress and Exit Code Semantics

### Streaming Progress Output (GUI Only)

When running commands with `--json`, the CLI may emit human-readable progress messages to stdout **before** the final JSON envelope. These messages are intended for real-time UI updates and follow these patterns:

**Common Progress Patterns:**
- `[OK] App.Id (driver: winget) - already installed`
- `[INSTALL] App.Id (driver: winget)`
- `[SKIP] App.Id - reason`
- `[FAIL] App.Id - error message`
- `[PLAN] App.Id - would install` (dry-run preview)

**GUI Parsing Rules:**
1. **Streaming progress is UI-only** - GUIs MAY parse these messages for live activity display
2. **JSON envelope is source of truth** - Final success/failure MUST be determined from the JSON envelope and exit code, NOT from streaming messages
3. **Streaming is best-effort** - Progress messages may be incomplete, out of order, or missing
4. **No semantic guarantees** - The format of progress messages may change; GUIs must not rely on them for business logic

### Exit Code Semantics

The CLI uses exit codes to signal overall command outcome:

| Exit Code | Meaning | JSON Envelope | GUI Behavior |
|-----------|---------|---------------|--------------|
| `0` | Success - all operations completed without errors | `success: true` | Show success state |
| `1` | Fatal error - command could not execute | `success: false`, `error` object populated | Show fatal error with error message |
| `2` | Partial failure - some apps failed but others succeeded | `success: false`, `error: null`, `counts.failed > 0` | Show "Completed with issues" with summary |

**Partial Failure Semantics:**
- Exit code `2` indicates partial success: some apps installed successfully, some failed
- JSON envelope will have `success: false` but `error: null`
- The `data.counts` object will show breakdown: `installed`, `alreadyInstalled`, `failed`, `skippedFiltered`
- GUIs MUST distinguish partial failures from fatal errors:
  - **Partial failure**: Show "Completed with issues" + summary (e.g., "60 installed • 1 failed")
  - **Fatal error**: Show "An error occurred" + error message

**Current Implementation Note:**
As of this writing, the CLI returns exit code `1` for both fatal errors and partial failures. GUIs should detect partial failures by checking if `error` is `null` and `counts.failed > 0`, treating these as "completed with issues" rather than fatal errors.

---
## Error Codes

Standard error codes for programmatic handling:

| Code | Description |
|------|-------------|
| `MANIFEST_NOT_FOUND` | Manifest file does not exist |
| `MANIFEST_PARSE_ERROR` | Manifest is invalid JSON/JSONC |
| `MANIFEST_VALIDATION_ERROR` | Manifest schema validation failed |
| `PLAN_NOT_FOUND` | Plan file does not exist |
| `PLAN_PARSE_ERROR` | Plan file is invalid |
| `WINGET_NOT_AVAILABLE` | winget not installed |
| `INSTALL_FAILED` | Package installation failed |
| `RESTORE_FAILED` | Configuration restore failed |
| `VERIFY_FAILED` | Verification check failed |
| `PERMISSION_DENIED` | Insufficient permissions |
| `INTERNAL_ERROR` | Unexpected internal error |
| `SCHEMA_INCOMPATIBLE` | Schema version mismatch |

---

## CLI Flag Styles

### Manifest Path Resolution

The `--profile` and `--manifest` parameters accept either:

1. **Profile name** (simple string without path separators or file extensions)
   - Resolved under the engine's `manifests/` directory
   - Example: `--profile hugo-laptop` → `<repo>/manifests/hugo-laptop.jsonc`
   - Backward compatible with existing workflows

2. **File path** (detected by any of the following heuristics)
   - Contains path separator (`/` or `\`)
   - Has `.json`, `.jsonc`, or `.json5` extension
   - Exists as a file at the specified path
   - Can be absolute or relative (relative paths resolved to absolute)
   - Example: `--profile "C:\Users\...\Setups\setup_2025-12-22.jsonc"` → used directly

This allows GUIs to pass full file paths to scanned setups in user directories (e.g., `Documents\Endstate\Setups`) while maintaining backward compatibility with profile name resolution.



Endstate CLI supports **two flag styles** for maximum compatibility:

### PowerShell-style Flags (native)
```powershell
Endstate apply -Profile Hugo-Laptop -Json
Endstate verify -Manifest path/to/manifest.jsonc -Json
Endstate report -Out report.json -Json
```

### GNU-style Flags (cross-platform)
```powershell
Endstate apply --profile Hugo-Laptop --json
Endstate verify --manifest path/to/manifest.jsonc --json
Endstate report --out report.json --json
```

**Supported GNU-style Flags:**
- `--json` → `-Json` 
- `--profile` → `-Profile` 
- `--manifest` → `-Manifest` 
- `--out` → `-Out` 
- `--latest` → `-Latest`
- `--runid` → `-RunId`
- `--last` → `-Last`
- `--dry-run` → `-DryRun`
- `--enable-restore` → `-EnableRestore`
- `--help` → shows help

Both styles can be mixed in the same command and are functionally identical. This dual support ensures compatibility with GUI tools, CI/CD pipelines, and cross-platform automation scripts.

---

## JSON Output Contract

When `--json` or `-Json` is specified:

### Stdout Purity
- **stdout contains JSON ONLY** - no banner, no progress text, no `Write-Host` output
- Human-readable messages (banner, warnings, errors) go to non-stdout streams (Information/Verbose/Warning/Error) or are suppressed
- This ensures GUI tools can reliably parse stdout as JSON without filtering

---

## How to Run

Endstate supports **two invocation styles**:

### 1. Direct Invocation (from repo directory)
```powershell
.\cli.ps1 -Command <command> [options]
```

### 2. PATH-Installed Invocation (from anywhere)
```powershell
Endstate <command> [options]
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
| `capabilities` | Report CLI capabilities for GUI handshake |

---

## Canonical Workflow

```powershell
# 1. CAPTURE: Export current machine state
.\cli.ps1 -Command capture -Profile my-machine
# or: Endstate capture -Profile my-machine

# 2. PLAN: Preview what would be applied
.\cli.ps1 -Command plan -Manifest manifests/my-machine.jsonc
# or: Endstate plan -Manifest manifests/my-machine.jsonc

# 3. APPLY: Execute the plan (use -DryRun first!)
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc -DryRun
.\cli.ps1 -Command apply -Manifest manifests/my-machine.jsonc
# or: Endstate apply -Manifest manifests/my-machine.jsonc

# 4. VERIFY: Confirm desired state is achieved
.\cli.ps1 -Command verify -Manifest manifests/my-machine.jsonc
# or: Endstate verify -Manifest manifests/my-machine.jsonc

# 5. DOCTOR: Check environment health
.\cli.ps1 -Command doctor
# or: Endstate doctor
```

**Note**: Restore is **opt-in** and requires explicit `-EnableRestore` flag.

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

## GUI Development Prerequisites (Windows)

Endstate GUI is built with Tauri and requires the following on Windows:

- **Rust** via [rustup](https://rustup.rs/) (provides `cargo` and `rustc`)
- **Microsoft Visual C++ Build Tools** with MSVC and Windows SDK
- **Node.js** (v18+) and npm

These are required for Tauri's native compilation. See the [Tauri prerequisites guide](https://tauri.app/start/prerequisites/) for details.

---

## Not Yet Implemented

The following are planned but not yet functional:

- **apt/dnf/brew drivers** — Linux/macOS package managers
- **Verifier modules** — Custom verification beyond file-exists
- **Reboot handling** — Automatic reboot detection and `--reboot-if-needed`
- **Rollback** — Undo last apply using backup state

---

## References

- [README.md](../../README.md) - Project overview and quickstart
- [VISION.md](../../VISION.md) - Design philosophy and goals
- [WARP.md](../../WARP.md) - Development roadmap and milestones
- [CLI JSON Contract](../../docs/cli-json-contract.md) - Full schema specification
- [GUI Integration Contract](../../docs/gui-integration-contract.md) - Detailed integration guide


---

## Ruleset Editing Policy (MANDATORY)

### Canonical Ruleset Path

This ruleset file has ONE canonical path and MUST NEVER be inferred or guessed:

```
C:\Users\win-laptop\Desktop\projects\Endstate\.windsurf\rules\project-ruleset.md
```

### Mandatory Preflight Check

Before ANY edit/write/patch operation on this ruleset, you MUST verify the path is a leaf file:

```powershell
$Path = "C:\Users\win-laptop\Desktop\projects\Endstate\.windsurf\rules\project-ruleset.md"
if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
  throw "Expected leaf file not found: $Path"
}
```

If this check fails, STOP and resolve the correct file path before proceeding.

### Blessed Editing Method

The ruleset MUST ALWAYS be edited via PowerShell file operations:

```powershell
# Read
$content = Get-Content -LiteralPath $Path -Raw -Encoding UTF8

# Modify in-memory
$content = $content -replace 'old', 'new'

# Write back
Set-Content -LiteralPath $Path -Value $content -Encoding UTF8 -NoNewline
```

**NEVER use:**
- Editor tools (edit, multi_edit, etc.) on `.windsurf\rules\` files
- Patch tools
- Manual guessing of paths
- Directory paths instead of file paths

### No-Confirmation Rule

If a task requires updating this ruleset (e.g., CLI flags changed, output contracts changed, workflow changed):
- Update the ruleset automatically
- Do NOT ask "Should I update the ruleset?"
- Include the ruleset update in the same commit as the code change

### Enforcement

Failure to follow this policy results in:
- "Access prohibited" errors from editor tools
- Incomplete documentation
- Contract drift between code and docs


## Canonical paths (do not guess)

Project ruleset file (canonical, always edit this exact file when updating rules):
C:\Users\win-laptop\Desktop\projects\Endstate\.windsurf\rules\project-ruleset.md
