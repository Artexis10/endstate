# Project Rules: Endstate

## 1. Scope and Authority

This document defines **repo-specific operational policy** for the Endstate CLI project.

**Authority hierarchy:**
1. `docs/ai/AI_CONTRACT.md` — global AI behavior contract (highest authority)
2. `docs/ai/PROJECT_SHADOW.md` — architectural truth, invariants, landmines, non-goals
3. `docs/ai/PROJECT_RULES.md` — this document (operational policy)

This document complements the above; it does not override them.

---

## 2. Protected Areas and Change Boundaries

### Safe to Change (with normal review)
- `engine/*.ps1` — core orchestration logic
- `drivers/*.ps1` — package manager adapters
- `restorers/*.ps1` — configuration restoration modules
- `verifiers/*.ps1` — state verification modules
- `modules/apps/*/module.jsonc` — config module definitions
- `tests/unit/*.Tests.ps1` — unit tests
- `manifests/examples/` — shareable example manifests
- `manifests/includes/` — reusable manifest fragments

### Requires Explicit Instruction
- `bin/endstate.ps1` — CLI entrypoint (public interface)
- `docs/contracts/*.md` — integration contracts (CLI JSON, GUI, events, profiles)
- `.github/workflows/` — CI configuration

### Architectural Review Required
- Adding new drivers, restorers, or verifiers
- Changes to manifest schema (`version` field changes)
- Changes to JSON output envelope structure
- Changes to event contract schema
- Module system changes (bundles, config modules)

### Never Modify Without Explicit Request
- `docs/ai/AI_CONTRACT.md`
- `docs/ai/PROJECT_SHADOW.md`
- `LICENSE`, `NOTICE`

---

## 3. Environment and Config Contract

### Canonical Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_ALLOW_DIRECT` | Bypass entrypoint guard (set to `1`) |
| `ENDSTATE_TESTMODE` | Enable test mode |
| `ENDSTATE_ENTRYPOINT` | Set by CMD shim to verify invocation path |
| `ENDSTATE_WINGET_SCRIPT` | Override winget script for testing |

### Forbidden Patterns
- Hardcoded API keys or secrets
- Hardcoded absolute paths (use `$PSScriptRoot` or environment variables)
- Direct `ConvertFrom-Json` on manifests/plans (use `Read-JsoncFile`)

### Key Invariants
- JSONC is the preferred manifest format (`.jsonc`)
- All manifest parsing uses `Read-JsoncFile` for comment stripping
- Line endings normalized CRLF→LF for cross-platform hash consistency

---

## 4. Docker / Runtime Contract

**Not applicable.** Endstate is a PowerShell CLI tool with no Docker dependencies.

Runtime requirements:
- PowerShell 5.1+ (Windows PowerShell or PowerShell Core)
- winget (Windows Package Manager) for app installation

---

## 5. Data / Migrations / Storage Contract

### State Storage
- **Location:** `.endstate/state.json` (gitignored)
- **Backups:** `state/backups/<timestamp>/` (gitignored)
- **Run history:** `state/runs/<runId>.json` (gitignored)

### State Write Invariants
- State writes use temp file + atomic move pattern
- State schema includes `schemaVersion` field for future migrations
- No automatic migrations; schema changes require explicit handling

### Gitignored Runtime Directories
- `logs/` — execution logs
- `plans/` — generated execution plans
- `state/` — run history and checksums
- `.endstate/` — local state
- `manifests/local/` — machine-specific captures

---

## 6. Testing and Verification Contract

### Verification Entrypoint

The canonical verification command for this repository is:

```powershell
.\scripts\test-unit.ps1
```

This is the **required entrypoint** for CI and pre-commit verification. Exit code 0 indicates success.

### Push-Safe Tests (CI)
- All tests in `tests/unit/` are hermetic and CI-safe
- No real winget installs; all external calls mocked
- Deterministic and idempotent

### Integration Tests (Local Only)
- `tests/Endstate.Tests.ps1` — may require real environment
- `sandbox-tests/` — sandbox environment tests

### Test Commands

| Command | Purpose |
|---------|---------|
| `.\scripts\test-unit.ps1` | Run all unit tests (RECOMMENDED) |
| `.\scripts\test-unit.ps1 -Path tests\unit\Manifest.Tests.ps1` | Run specific test file |
| `.\scripts\test_pester.ps1 -Path tests/unit` | Legacy runner |

### Pester Version Contract
- **Required:** Pester 5.0.0 or higher
- **Vendored:** `tools/pester/Pester/5.7.1/` (committed)
- **Forbidden:** Direct `Invoke-Pester` calls (may load system Pester 3.x)

### Verification Rules
- Run only minimum targeted verification needed
- Do not run full test suites unless explicitly requested
- Provide copy-pastable commands when verification cannot be run

---

## 7. Frontend / UI / API Rules

### CLI JSON Output Contract
- All `--json` output includes standard envelope with `schemaVersion`, `cliVersion`, `command`, `runId`, `timestampUtc`, `success`, `data`, `error`
- JSON envelope is single-line compressed JSON on last line of stdout
- Breaking changes require schema major version bump AND CLI major version bump

### GUI Integration Rules
- GUI is thin presentation layer; CLI is source of truth
- GUI must perform capabilities handshake before any command
- GUI must validate `schemaVersion` compatibility

### Prohibited Patterns
- Business logic in GUI
- Direct file system operations for provisioning in GUI
- Assumptions about internal CLI implementation in GUI

---

## 8. Tooling and File Write Constraints

### PowerShell Set-Content Fallback
When standard file write tools fail, use PowerShell fallback:

```powershell
$Path = "<target-file>"
if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
  throw "Expected leaf file not found: $Path"
}
$content = Get-Content -LiteralPath $Path -Raw -Encoding UTF8
# Modify $content
Set-Content -LiteralPath $Path -Value $content -Encoding UTF8 -NoNewline
```

### File Write Rules
- Treat inability to write files as a bug to work around
- Never claim changes are applied unless file contents are actually written and confirmed
- Do not create files outside the project directory without explicit permission

### Git Commit Policy
- Never commit runtime artifacts (`logs/`, `plans/`, `state/`)
- Never commit local test manifests (`manifests/local/`, `*-smoke*.jsonc`, `test-*.jsonc`)
- Never bypass git hooks (`--no-verify` is forbidden)

### Documentation Drift Rule
- CLI command/flag changes require README.md and ruleset updates in same commit
- Contract changes require both repos updated (Endstate + endstate-gui)

---

## 9. OpenSpec Enforcement

This repository enforces **OpenSpec Level 2** (workflow gate).

### Setup

```powershell
npm install
npm run hooks:install
```

### Scripts

| Command | Purpose |
|---------|--------|
| `npm run hooks:install` | Install lefthook pre-push hook |
| `npm run openspec:list` | List all specs and changes |
| `npm run openspec:validate` | Validate all specs (strict mode) |
| `npm run openspec:validate:ci` | Validation via PowerShell wrapper |

### Pre-Push Hook

The pre-push hook is managed by **lefthook** (repo-tracked in `lefthook.yml`). Do not rely on `.git/hooks/` files directly.

Validation failure blocks the push.

**Emergency bypass:** Set `OPENSPEC_BYPASS=1` environment variable. Use sparingly and only for non-behavior changes.

### Adding Specs

Behavior specifications live in `openspec/specs/`. See `docs/runbooks/OPENSPEC_ENFORCEMENT.md` for workflow.

---

## 10. References

- [AI_CONTRACT.md](./AI_CONTRACT.md) — global AI behavior contract
- [PROJECT_SHADOW.md](./PROJECT_SHADOW.md) — architectural truth
- [OpenSpec Enforcement Runbook](../runbooks/OPENSPEC_ENFORCEMENT.md) — spec workflow
- [CLI JSON Contract](../contracts/cli-json-contract.md) — JSON output schema
- [GUI Integration Contract](../contracts/gui-integration-contract.md) — GUI ↔ CLI rules
- [Event Contract](../contracts/event-contract.md) — streaming events schema
- [Profile Contract](../contracts/profile-contract.md) — profile resolution rules
