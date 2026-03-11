## Context

Phase 1 of the Go engine rewrite established the CLI framework (`cmd/endstate/main.go`), JSON envelope output (`internal/envelope/`), NDJSON event emission (`internal/events/`), JSONC manifest loading (`internal/manifest/`), winget driver (`internal/driver/winget/`), and three command handlers (capabilities, verify, apply). Phase 2 adds the remaining commands: capture, plan, report, doctor, and profile subcommands, plus supporting packages for state persistence, system snapshots, and execution planning.

All contracts (`cli-json-contract.md`, `event-contract.md`, `profile-contract.md`, `capture-artifact-contract.md`, `gui-integration-contract.md`) define the authoritative output shapes. The PowerShell implementation is the behavioral reference.

## Goals / Non-Goals

**Goals:**
- Implement capture command with system snapshot, sanitize, discover, update, and profile output modes
- Implement state persistence with atomic temp+rename write pattern
- Implement plan command as standalone diff computation
- Implement report command with run history retrieval (--latest, --last N, --run-id)
- Implement doctor command checking system prerequisites
- Implement profile subcommands (list, path, validate) per profile-contract.md
- Wire all commands into main.go dispatch, help text, and capabilities response
- Achieve test coverage for all new packages

**Non-Goals:**
- Restore command implementation (Phase 3)
- Bundle/zip profile output (Phase 3)
- Config module expansion (Phase 3)
- Bootstrap command (Phase 3)
- Modifying any PowerShell files
- Changing any contract or spec behavior

## Decisions

### 1. Snapshot parsing via column-position detection
**Decision:** Parse `winget list` tabular output by detecting header column positions (Name, ID, Version, Source) from the header row, then extracting fixed-width substrings.
**Rationale:** Winget output is tab-aligned text, not structured data. The PowerShell engine uses regex splitting on 2+ spaces, but column-position detection is more reliable for edge cases where app names contain multiple spaces. The `winget list --source winget` flag filters to winget-sourced packages only.
**Alternative considered:** Regex splitting on `\s{2,}` — rejected due to fragility with display names containing multiple spaces.

### 2. Atomic state writes via temp file + os.Rename
**Decision:** All state file writes use the pattern: write to `<target>.tmp` in the same directory, then `os.Rename()` to the target path.
**Rationale:** Matches the PowerShell engine's `Out-File -FilePath $tempFile` + `Move-Item` pattern. On Windows, `os.Rename` within the same directory is atomic at the filesystem level. This prevents partial writes from corrupting state.

### 3. ExecCommand injection for snapshot testing
**Decision:** The snapshot package uses an injectable `ExecCommand` function field (same pattern as the winget driver in Phase 1) to enable test mocking of `winget list` output.
**Rationale:** Follows the established Phase 1 pattern in `internal/driver/winget/`. Avoids requiring actual winget binary during tests.

### 4. Profile subcommands via nested arg parsing
**Decision:** When `main.go` dispatches to `"profile"`, the remaining args are passed to a `RunProfile(subcommand, args)` handler that does a secondary `switch` on the subcommand (list, path, validate).
**Rationale:** Keeps `main.go` dispatch simple while allowing profile-specific arg parsing. Avoids over-engineering a subcommand framework for a single command.

### 5. Planner reuses driver.Driver interface
**Decision:** `planner.ComputePlan()` accepts a `driver.Driver` parameter and calls `Detect()` for each app — the same pattern used by apply's plan phase.
**Rationale:** The plan command is functionally equivalent to apply's Phase 1 (plan) without Phases 2-3 (apply/verify). Reusing the driver interface enables mock injection for tests and ensures consistent detection behavior.

### 6. Capture writes JSONC manifests (bare format only)
**Decision:** Phase 2 capture writes bare `.jsonc` manifests only. Zip bundle output is deferred to Phase 3.
**Rationale:** Zip bundles require config module expansion and payload staging, which are Phase 3 concerns. Bare manifest capture is fully functional and matches the `--discover` workflow.

## Risks / Trade-offs

- **[Risk] Winget output format may change across versions** → Mitigation: Column-position detection is header-driven and adapts to column width changes. Tests use realistic fixtures that can be updated.
- **[Risk] main.go merge conflicts from three parallel teammates** → Mitigation: Each teammate owns distinct `case` branches. Integration step resolves any conflicts.
- **[Risk] State directory may not exist on first run** → Mitigation: All state write paths create parent directories with `os.MkdirAll()` before writing.
- **[Risk] Doctor checks spawn external processes that may hang** → Mitigation: Use `context.WithTimeout` (5 second timeout) for all subprocess calls in doctor checks.
