# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Endstate is a declarative system provisioning and recovery tool for Windows. It eliminates the "clean install tax" by enabling repeatable machine rebuilds from a single manifest. Primary language is Go; the engine lives in `go-engine/`.

## Governance Documents (Read These First)

This repo has an explicit authority hierarchy for AI collaborators:

1. `docs/ai/AI_CONTRACT.md` — AI behavior contract (highest authority)
2. `docs/ai/PROJECT_RULES.md` — operational policy (env vars, testing, protected areas)
3. `CLAUDE.md` — architecture context, commands, landmines (this file, auto-loaded by Claude Code)
4. `openspec/specs/` — invariants and behavior specifications (lazy-loaded on demand)

Key rules: make the smallest change satisfying acceptance criteria; no unrelated refactors or formatting sweeps; contract-first edits (schema → implementation → tests); significant changes must be represented in OpenSpec specs.

## Commands

```bash
# Run all unit tests (canonical verification command)
cd go-engine && go test ./...

# Run a specific test package
cd go-engine && go test ./internal/manifest/...

# Validate OpenSpec specs
npm run openspec:validate

# Install git hooks (lefthook pre-push)
npm run hooks:install

# Build and run CLI in dev mode
cd go-engine && go run ./cmd/endstate <command>
```

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

- **`go-engine/cmd/endstate/`** — CLI entrypoint (Go binary)
- **`go-engine/internal/`** — Core engine packages:
  - `manifest/` — Manifest loading, include resolution, JSONC stripping
  - `commands/` — CLI command implementations (apply, capture, restore, verify, etc.)
  - `modules/` — Config module catalog loading, validation, manifest expansion
  - `planner/` — Execution plan generation and diff computation
  - `driver/` — Package manager adapters (winget is primary)
  - `restore/` — Config restoration strategies (copy, merge-json, merge-ini, append)
  - `verifier/` — State assertions (file-exists, command-exists, registry-key-exists)
  - `events/` — Streaming event emission (JSONL)
  - `envelope/` — JSON output envelope construction
  - `snapshot/` — System snapshot for capture
  - `config/` — Configuration and path resolution
  - `bundle/` — Bundle loading and module grouping
- **`modules/apps/<id>/module.jsonc`** — Reusable config module definitions with matches, verify, restore, capture sections
- **`payload/apps/<id>/`** — Staged configuration files referenced by modules
- **`bundles/`** — Named module groupings (JSONC)
- **`manifests/`** — Desired state declarations (`examples/` shareable, `includes/` reusable fragments, `local/` gitignored machine-specific)

## Critical Landmines

1. **JSONC parsing**: Always use `StripJsoncComments` (in `go-engine/internal/manifest/`) — never raw `json.Unmarshal` on `.jsonc` files
2. **Capture zip layout**: Capture stages files under `configs/<module-id>/` but module definitions use `./payload/apps/<id>/` — path rewriting must reconcile these
3. **Directory copy nesting**: When copying directories, ensure the destination is removed first for idempotent behavior — copying into an existing directory nests the source inside it
4. **Line endings**: Manifest hashes normalize CRLF→LF for cross-platform consistency
5. **Stale bootstrapped copies**: The GUI (and PATH-based invocations) run the bootstrapped copy at `%LOCALAPPDATA%\Endstate\bin\`, not the repo. New engine features won't appear in the GUI until re-bootstrapped. Always run `endstate bootstrap` after engine changes. The GUI's `predev` npm hook handles this for `npm run dev` / `tauri dev`.
6. **Winget database lock contention**: Concurrent winget operations (or rapid successive calls) can fail due to SQLite lock contention on the winget database. Capture retries once on 0-app results to handle this.
7. **Batch vs per-ref display name differences**: `DetectBatch` returns display names from winget's local database which may differ from per-ref `winget show` output. Use batch results as authoritative for installed app names.
8. **Manual app `launch`/`instructions` are GUI metadata only**: The engine includes `launch` URLs and `instructions` text in manual app entries, but these are consumed exclusively by the GUI for display. The engine never opens URLs or displays instructions itself.

## Core Invariants

- **Idempotent**: Re-running converges without duplicating work
- **Non-destructive defaults**: No silent deletions; destructive ops require explicit flags
- **Restore is opt-in**: Requires `--enable-restore` flag
- **Verification-first**: Observable state is success, not "it ran"
- **Separation of concerns**: Install ≠ configure ≠ verify (distinct pipeline stages)
- **Backup before overwrite**: Files backed up to `state/backups/<timestamp>/`
- **CLI is source of truth**: GUI is thin presentation layer

## Testing

- **Framework**: Go standard `testing` package
- **Unit tests**: `go-engine/internal/*/` — all hermetic, CI-safe, no real winget calls; test files use `_test.go` suffix
- **Fixtures**: `tests/fixtures/` for shared test manifests and module definitions
- **CI**: GitHub Actions runs `cd go-engine && go test ./...` on windows-latest
- Run only minimum targeted verification needed; do not run full suite unless requested

## Protected Areas

- `go-engine/cmd/endstate/`, `docs/contracts/*.md`, `.github/workflows/` — require explicit instruction to modify
- `docs/ai/AI_CONTRACT.md`, `LICENSE`, `NOTICE` — never modify without explicit request

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_TESTMODE` | Enable test mode |

## OpenSpec

Behavior specs enforced at Level 2 (pre-push hook via lefthook). Specs live in `openspec/specs/`, changes in `openspec/changes/`. Emergency bypass: `OPENSPEC_BYPASS=1 git push`.

## Forbidden Patterns

- Hardcoded absolute paths (use relative paths or environment variables)
- Raw `json.Unmarshal` on `.jsonc` files (use `StripJsoncComments` first)
- Committing runtime artifacts (`logs/`, `plans/`, `state/`, `manifests/local/`)
- Bypassing git hooks (`--no-verify`)
