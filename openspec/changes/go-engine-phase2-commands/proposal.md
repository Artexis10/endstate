## Why

Phase 1 of the Go engine rewrite delivered the CLI framework, JSON envelope, NDJSON events, JSONC manifest loading, winget driver, and capabilities/verify/apply commands. Phase 2 completes the remaining CLI surface: capture, plan, report, doctor, and profile subcommands, plus the state persistence layer. This is a language rewrite — all existing contracts and OpenSpec specs are the acceptance gates. No behavior changes.

## What Changes

- Add `internal/snapshot/` package: system snapshot via `winget list`, display name map generation
- Add `internal/state/` package: atomic state file read/write (temp+rename), run history management
- Add `internal/planner/` package: diff computation between manifest and current system state
- Add `internal/commands/capture.go`: capture command with discover, sanitize, update, profile output modes
- Add `internal/commands/plan.go`: standalone plan command exposing diff as envelope
- Add `internal/commands/report.go`: run history retrieval with --latest, --last, --run-id filters
- Add `internal/commands/doctor.go`: system prerequisite checks (winget, PowerShell, profiles dir, state dir, engine version)
- Add `internal/commands/profile.go`: profile subcommands (list, path, validate) per profile-contract.md
- Wire all new commands into `cmd/endstate/main.go` dispatch and help text
- Update `internal/commands/capabilities.go` to reflect new command flags
- Add comprehensive tests for all new packages

## Capabilities

### New Capabilities

_(none — this is a language rewrite of existing capabilities, not new behavior)_

### Modified Capabilities

_(none — no spec-level behavior changes; the Go implementation must reproduce existing contract behavior exactly)_

## Impact

- All changes confined to `go-engine/` directory
- No PowerShell files modified
- No contract or spec changes required
- New Go packages: `snapshot`, `state`, `planner`
- New command files in existing `commands` package
- `main.go` dispatch updated for 5 new commands
- Test coverage for all new code
