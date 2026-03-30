# Project Rules: Endstate

## 1. Scope and Authority

This document defines **repo-specific operational policy** for the Endstate CLI project.

**Authority hierarchy:**
1. `docs/ai/AI_CONTRACT.md` — AI behavior contract (highest authority)
2. `docs/ai/PROJECT_RULES.md` — this document (operational policy)
3. `CLAUDE.md` — architecture context, commands, landmines (auto-loaded by Claude Code)
4. `openspec/specs/` — invariants and behavior specifications (lazy-loaded on demand)

This document complements the above; it does not override them.

---

## 2. Protected Areas and Change Boundaries

### Safe to Change (with normal review)
- `go-engine/internal/` — core engine packages (manifest, commands, driver, restore, verifier, etc.)
- `modules/apps/*/module.jsonc` — config module definitions
- `go-engine/internal/*_test.go` — unit tests
- `manifests/examples/` — shareable example manifests
- `manifests/includes/` — reusable manifest fragments

### Requires Explicit Instruction
- `go-engine/cmd/endstate/` — CLI entrypoint (public interface)
- `docs/contracts/*.md` — integration contracts (CLI JSON, GUI, events, profiles)
- `.github/workflows/` — CI configuration

### Architectural Review Required
- Adding new driver, restore, or verifier implementations
- Changes to manifest schema (`version` field changes)
- Changes to JSON output envelope structure
- Changes to event contract schema
- Module system changes (bundles, config modules)

### Never Modify Without Explicit Request
- `docs/ai/AI_CONTRACT.md`
- `LICENSE`, `NOTICE`

---

## 3. Environment and Config Contract

### Canonical Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_TESTMODE` | Enable test mode |

### Forbidden Patterns
- Hardcoded API keys or secrets
- Hardcoded absolute paths (use relative paths or environment variables)
- Raw `json.Unmarshal` on `.jsonc` files (use `StripJsoncComments` first)

### Key Invariants
- JSONC is the preferred manifest format (`.jsonc`)
- All manifest parsing uses `StripJsoncComments` for comment stripping before JSON decoding
- Line endings normalized CRLF→LF for cross-platform hash consistency

---

## 4. Docker / Runtime Contract

**Not applicable.** Endstate is a Go CLI tool with no Docker dependencies.

Runtime requirements:
- Go 1.22+ for building from source
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

```bash
cd go-engine && go test ./...
```

This is the **required entrypoint** for CI and pre-commit verification. Exit code 0 indicates success.

### Push-Safe Tests (CI)
- All tests in `go-engine/internal/` are hermetic and CI-safe
- No real winget installs; all external calls mocked or stubbed
- Deterministic and idempotent

### Test Commands

| Command | Purpose |
|---------|---------|
| `cd go-engine && go test ./...` | Run all unit tests (RECOMMENDED) |
| `cd go-engine && go test ./internal/manifest/...` | Run specific package tests |
| `cd go-engine && go test -v -run TestName ./internal/commands/...` | Run a specific test |

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

- [AI_CONTRACT.md](./AI_CONTRACT.md) — AI behavior contract
- [OpenSpec Enforcement Runbook](../runbooks/OPENSPEC_ENFORCEMENT.md) — spec workflow
- [CLI JSON Contract](../contracts/cli-json-contract.md) — JSON output schema
- [GUI Integration Contract](../contracts/gui-integration-contract.md) — GUI ↔ CLI rules
- [Event Contract](../contracts/event-contract.md) — streaming events schema
- [Profile Contract](../contracts/profile-contract.md) — profile resolution rules
