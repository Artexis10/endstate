# Project Shadow: Endstate

## 1. Identity

- **Project Name:** Endstate
- **One-line Purpose:** Declarative system provisioning and recovery tool that restores a machine to a known-good end state safely, repeatably, and without guesswork
- **Primary Language:** PowerShell
- **Repository Type:** CLI tool with engine library, designed for Windows (winget driver), with planned Linux/macOS support

---

## 2. Architecture Overview

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

### Key Directories

| Directory | Purpose |
|-----------|---------|
| `bin/` | CLI entrypoints (`endstate.ps1`, `endstate.cmd` shim) |
| `engine/` | Core orchestration logic (manifest, apply, capture, restore, verify, etc.) |
| `drivers/` | Software installation adapters (winget is primary) |
| `restorers/` | Configuration restoration modules (copy, merge-json, merge-ini, append) |
| `verifiers/` | State verification modules (file-exists, command-exists, registry-key-exists) |
| `modules/` | Config module catalog (`modules/apps/<app-id>/module.jsonc`) |
| `bundles/` | Reusable module groupings |
| `manifests/` | Desired state declarations (`examples/`, `includes/`, `local/` gitignored) |
| `tests/` | Pester unit tests |
| `docs/contracts/` | Integration contracts (CLI JSON, GUI, events, profiles) |

### Entry Points

- **CLI:** `bin/endstate.ps1` (invoked via `endstate.cmd` shim for proper stdout/stderr handling)
- **Commands:** `apply`, `capture`, `plan`, `verify`, `restore`, `export-config`, `revert`, `report`, `doctor`, `state`, `bootstrap`

---

## 3. Core Abstractions

### Central Types

- **Manifest:** JSONC/JSON/YAML declarative specification of desired state (apps, restore entries, verify steps)
- **Plan:** Computed execution plan from manifest diff against current state
- **Module:** Self-contained config restoration unit in `modules/apps/<id>/module.jsonc`
- **Bundle:** Named collection of modules for grouping
- **Driver:** Package manager adapter (winget, future: apt, brew)
- **Restorer:** Configuration application strategy (copy, merge-json, merge-ini, append)
- **Verifier:** State assertion (file-exists, command-exists, registry-key-exists)

### Data Flow

1. Manifest loaded and includes resolved (circular detection enforced)
2. Bundles/modules expanded into restore entries
3. Planner computes diff against current state
4. Drivers install missing apps
5. Restorers apply configuration (opt-in via `-EnableRestore`)
6. Verifiers confirm desired state achieved
7. State persisted to `.endstate/state.json`

### Naming Conventions

- Engine scripts: `<verb>.ps1` (e.g., `apply.ps1`, `capture.ps1`, `bundle.ps1`)
- Test files: `<Subject>.Tests.ps1`
- Modules: `modules/apps/<app-id>/module.jsonc`
- Manifests: `*.jsonc` (preferred), `*.json`, `*.yaml`
- Profiles: Three formats — `<name>.zip` (preferred), `<name>/manifest.jsonc` (folder), `<name>.jsonc` (bare)
- Capture output: Zip bundle at `Documents\Endstate\Profiles\<name>.zip` containing `manifest.jsonc`, `metadata.json`, and optional `configs/` directory

---

## 4. Invariants

1. **Idempotence:** Re-running any command converges to the same result without duplicating work
2. **Non-destructive defaults:** No silent deletions; destructive operations require explicit flags
3. **Verification-first:** "It ran" is not success; success means desired state is observable
4. **Separation of concerns:** Install ≠ configure ≠ verify (distinct pipeline stages)
5. **Backup before overwrite:** Existing files backed up before restoration (`state/backups/<timestamp>/`)
6. **Restore is opt-in:** Restore operations require `-EnableRestore` flag for safety
7. **CLI is source of truth:** GUI is thin presentation layer; all logic lives in CLI
8. **JSON schema versioning:** Breaking changes require schema major version bump

---

## 5. Contracts and Boundaries

### Public APIs / Stable Interfaces

| Contract | Location | Purpose |
|----------|----------|---------|
| CLI JSON Contract | `docs/contracts/cli-json-contract.md` | JSON output schema for `--json` flag |
| GUI Integration Contract | `docs/contracts/gui-integration-contract.md` | GUI ↔ CLI integration rules |
| Event Contract | `docs/contracts/event-contract.md` | Streaming events (`--events jsonl`) |
| Profile Contract | `docs/contracts/profile-contract.md` | Profile resolution rules |
| Config Portability Contract | `docs/contracts/config-portability-contract.md` | Cross-machine config portability |

### Internal Boundaries

- `engine/` scripts are internal; CLI is the public interface
- `drivers/` implement `driver.ps1` interface
- `restorers/` implement restore entry handling
- `verifiers/` implement verification checks

### Integration Points

- **winget:** Primary package manager driver
- **Endstate GUI:** Separate commercial product consuming CLI via JSON output
- **CI:** GitHub Actions workflow (`.github/`)

---

## 6. Landmines

1. **Entrypoint guard:** `endstate.ps1` must be invoked via `endstate.cmd` shim for proper stdout/stderr redirection; direct invocation is blocked unless `$env:ENDSTATE_ALLOW_DIRECT = '1'`
2. **Circular includes:** Manifest includes are tracked; circular references throw clear errors
3. **Config module expansion:** `$script:ExpandConfigModules` flag controls whether modules are expanded during manifest loading; tests may disable this
4. **State file atomicity:** State writes use temp file + move pattern for atomic updates
5. **PATH installation:** Bootstrap installs to `%LOCALAPPDATA%\Endstate\bin\lib\` (not `bin\` directly) to ensure CMD shim takes precedence over PowerShell's `.ps1` preference
6. **JSONC parsing:** Comments must be stripped before JSON parsing; use `Read-JsoncFile` helper
7. **Line ending normalization:** Manifest hashes normalize CRLF→LF for cross-platform consistency
8. **Windows Sandbox WDAC/Smart App Control blocks unsigned DLLs.** Apps with unsigned native binaries (Git's `msys-2.0.dll`, AutoHotkey, KeePassXC, etc.) hang in sandbox with modal "Bad Image" dialogs that no one can dismiss, causing 600s timeouts. The inner sandbox script (`sandbox-tests/discovery-harness/sandbox-validate.ps1`) disables this at Stage 0 via `Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy" -Name "VerifiedAndReputablePolicyState" -Value 0` followed by `CiTool.exe -r`. This must run before winget bootstrap and any installs.

---

## 7. Non-Goals

1. **Enterprise configuration management:** This is a personal/small-team tool, not enterprise software
2. **Cross-platform parity (yet):** Windows/winget is primary; Linux/macOS support is future work
3. **Package version pinning:** MVP does not compare or pin versions
4. **Streaming progress (yet):** `--events jsonl` is implemented but streaming is marked as `false` in capabilities
5. **Automatic rollback:** Failed operations do not auto-rollback; manual `revert` command exists for restore operations only
6. **GUI business logic:** GUI must not contain provisioning logic; CLI is source of truth

---

## 8. Testing Strategy

### Test Organization

| Location | Purpose |
|----------|---------|
| `tests/unit/` | Unit tests for engine modules |
| `tests/fixtures/` | Test data (manifests, plans) |
| `tests/contract/` | Contract compliance tests |
| `tests/Endstate.Tests.ps1` | Integration tests |

### Test Framework

- **Pester 5.7.1** vendored in `tools/pester/` for deterministic, offline-capable testing

### What Must Be Tested

- JSON schema shape (envelope fields, error codes)
- Manifest parsing (includes, circular detection, format support)
- Plan generation (diff computation)
- Restore strategies (copy, merge, append)
- State persistence (atomic writes)

### Test Commands

```powershell
# Run all tests
.\scripts\test_pester.ps1

# Run specific suite
.\scripts\test_pester.ps1 -Path tests/unit

# Run with tag filter
.\scripts\test_pester.ps1 -Tag "Manifest"
```

---

## 9. Development Workflow

### Build

No build step required; PowerShell scripts run directly.

### Run

```powershell
# Bootstrap (one-time): Install endstate to PATH
.\bin\endstate.ps1 bootstrap

# After bootstrap, use from anywhere:
endstate <command> [options]

# Or run directly (requires bypass):
$env:ENDSTATE_ALLOW_DIRECT = '1'
.\bin\endstate.ps1 <command>
```

### Environment Setup

| Requirement | Version | Purpose |
|-------------|---------|---------|
| PowerShell | 5.1+ | Script execution |
| winget | Latest | App installation (Windows) |

### Key Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_ALLOW_DIRECT` | Bypass entrypoint guard |
| `ENDSTATE_TESTMODE` | Enable test mode |
| `ENDSTATE_ENTRYPOINT` | Set by CMD shim to verify invocation path |

---

## 10. Authority Model

### Decision Ownership

- **Architecture:** Hugo Ander Kivi (author/maintainer)
- **Code review:** Single maintainer; no formal review process documented
- **Releases:** Manual; VERSION.txt generated at build time

### Escalation

- Single maintainer model; no escalation path documented
- Issues via GitHub

### Related Projects

- **automation-suite:** Parent repository (Endstate was extracted from `provisioning/` subsystem)
- **endstate-gui:** Separate commercial GUI product (thin presentation layer)

---

*Generated: 2026-01-08*
*Shadow Spec Version: 1.0*
