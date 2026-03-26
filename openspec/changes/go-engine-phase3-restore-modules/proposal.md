## Why

The Go engine rewrite (Phases 1–2) covers CLI framework, envelope, events, manifest loading, driver, snapshot, and commands for capabilities/apply/verify/capture/plan/report/doctor/profile. Phase 3 completes the rewrite by implementing restore, config modules, bundles, verifiers, export, revert, and bootstrap — the remaining commands needed for full parity with the PowerShell engine.

## What Changes

- Implement four restore strategies: copy (file + directory with exclude globs, locked file handling), merge-json (deep recursive merge), merge-ini (section-aware merge), and append
- Add backup-before-overwrite safety with SHA256 up-to-date detection
- Add restore journaling (logs/restore-journal-<runId>.json) and journal-based revert
- Wire `--enable-restore` into the apply command's execution pipeline (after install, before verify)
- Implement config module catalog loading from modules/apps/*/module.jsonc
- Implement configModules expansion in manifests (replace references with actual restore/verify entries)
- Implement bundle zip creation (manifest + metadata + configs/<module-id>/ layout) and extraction
- Implement source path rewriting (./payload/apps/<id>/ → ./configs/<module-id>/) in bundles
- Add three verifier types: file-exists, command-exists, registry-key-exists
- Wire verifiers into the verify command alongside app installation checks
- Add standalone restore, revert, export-config, validate-export, and bootstrap commands
- Update capabilities response with all new commands

## Capabilities

### New Capabilities
- `go-restore-engine`: Four restore strategies (copy, merge-json, merge-ini, append), backup safety, sensitive path detection, env var expansion, source resolution (Model B), dry-run support
- `go-restore-journal`: Restore journal writing and journal-based revert (reverse-order processing)
- `go-config-modules`: Module catalog loading, app-to-module matching, configModules expansion in manifests
- `go-bundle-zip`: Bundle zip creation with config collection and path rewriting, bundle extraction
- `go-verifiers`: Verifier dispatch for file-exists, command-exists, registry-key-exists
- `go-export-commands`: export-config (system → export dir), validate-export (check export completeness)
- `go-bootstrap`: Bootstrap command — copy binary, create CMD shim, add to PATH

### Modified Capabilities
- `capture-bundle-zip`: Go implementation must match zip layout contract (configs/<module-id>/ structure, path rewriting)
- `restore-filter`: Go implementation must honor restore filtering rules from existing spec
- `capture-artifact-contract`: Go capture must produce bundles matching the artifact contract

## Impact

- All changes in `go-engine/` — no PowerShell files modified
- New packages: `internal/restore/`, `internal/modules/`, `internal/bundle/`, `internal/verifier/`
- New commands wired in `cmd/endstate/main.go`: restore, revert, export-config, validate-export, bootstrap
- Modified: `internal/commands/apply.go` (restore wiring), `internal/commands/verify.go` (verifier wiring), `internal/commands/capabilities.go` (new commands)
- New dependency: `golang.org/x/sys/windows/registry` for registry verifier
- Existing contracts are acceptance gates: cli-json-contract, event-contract, config-portability-contract, restore-safety-contract, capture-artifact-contract
