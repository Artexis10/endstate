# CLAUDE.md

## Project Overview

Endstate is a declarative system provisioning and recovery tool for Windows. It enables repeatable machine rebuilds from a single manifest. The primary language is Go; the engine lives in `go-engine/`.

## Governance Documents (Read These First)

This repo has an explicit authority hierarchy for AI collaborators:

1. `docs/ai/AI_CONTRACT.md` — AI behavior contract (highest authority)
2. `docs/ai/PROJECT_RULES.md` — operational policy (env vars, testing, protected areas)
3. `CLAUDE.md` — architecture context, commands, landmines (this file, auto-loaded by Claude Code)
4. `openspec/specs/` — invariants and behavior specifications (lazy-loaded on demand)

Make the smallest change satisfying acceptance criteria. Do not make unrelated refactors or formatting sweeps. Use contract-first edits (schema → implementation → tests); significant changes must be represented in OpenSpec specs.

## Commands

```bash
cd go-engine && go test ./...
cd go-engine && go test ./internal/manifest/...
npm run openspec:validate
npm run hooks:install
cd go-engine && go run ./cmd/endstate <command>
```

## Architecture

```
Spec → Planner → Drivers → Restorers → Verifiers → Reports/State
```

- `go-engine/cmd/endstate/` — CLI entrypoint
- `go-engine/internal/` — core engine packages
- `modules/apps/<id>/module.jsonc` — reusable config module definitions
- `payload/apps/<id>/` — staged configuration files
- `bundles/` — named module groupings
- `manifests/` — desired state declarations (`examples/` shareable, `includes/` reusable fragments, `local/` gitignored machine-specific)

## Critical Landmines

1. Always use `StripJsoncComments` in `go-engine/internal/manifest/`; never raw `json.Unmarshal` on `.jsonc`.
2. Capture stages `configs/<module-id>/`, while definitions use `./payload/apps/<id>/`; reconcile path rewriting.
3. Remove a directory destination before copying for idempotency, or the source nests inside it.
4. Manifest hashes normalize CRLF→LF.
5. GUI and PATH invocations use `%LOCALAPPDATA%\Endstate\bin\`; re-bootstrap after engine changes. GUI `predev` does this for `npm run dev` / `tauri dev`.
6. Winget SQLite locks can make concurrent or rapid calls fail; capture retries once on 0-app results.
7. `DetectBatch` local-database display names can differ from `winget show`; batch results are authoritative.
8. Manual app `launch`/`instructions` are GUI metadata only; the engine never opens URLs or displays them.

## Core Invariants

- Idempotent: re-running converges without duplication
- Non-destructive defaults: no silent deletions; destructive ops need explicit flags
- Restore is opt-in: `--enable-restore`
- Verification-first: observable state is success
- Install ≠ configure ≠ verify
- Back up before overwrite to `state/backups/<timestamp>/`
- CLI is source of truth; GUI is thin presentation

## Testing

Go standard `testing`; unit tests in `go-engine/internal/*/` are hermetic and CI-safe, with shared fixtures in `tests/fixtures/`. CI runs `cd go-engine && go test ./...` on `windows-latest`. Run minimum targeted verification; do not run the full suite unless requested.

## Protected Areas

- `go-engine/cmd/endstate/`, `docs/contracts/*.md`, `.github/workflows/` require explicit instruction.
- `docs/ai/AI_CONTRACT.md`, `LICENSE`, and `NOTICE` are never modified without explicit request.

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ENDSTATE_ROOT` | Override repo root path |
| `ENDSTATE_TESTMODE` | Enable test mode |

## OpenSpec

Behavior specs are enforced at Level 2 by the lefthook pre-push hook. Specs live in `openspec/specs/`, changes in `openspec/changes/`. Emergency bypass: `OPENSPEC_BYPASS=1 git push`.

## Forbidden Patterns

- Hardcoded absolute paths
- Raw `json.Unmarshal` on `.jsonc`
- Runtime artifacts: `logs/`, `plans/`, `state/`, `manifests/local/`
- `--no-verify`

## Specialized Agent Definitions

Before starting the corresponding task, read the matching role definition. Each role also follows the governance hierarchy above and `docs/ai/PROJECT_SHADOW.md`.

- Module creation or capture/restore work: [`.claude/agents/module-author.md`](.claude/agents/module-author.md)
- Go unit tests or regressions: [`.claude/agents/test-writer.md`](.claude/agents/test-writer.md)
- Contract, spec, or invariant review: [`.claude/agents/contract-guard.md`](.claude/agents/contract-guard.md)
- Core engine package changes: [`.claude/agents/engine-dev.md`](.claude/agents/engine-dev.md)
- Catalog-wide module validation or pre-release audits: [`.claude/agents/module-validator.md`](.claude/agents/module-validator.md)
