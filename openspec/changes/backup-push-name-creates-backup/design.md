## Context

`resolveBackupID(ctx, store, backupID, name)` in `go-engine/internal/backup/upload/upload.go` chooses the backup a push writes a version to. Prior logic: explicit id → use it; else if any backup exists → `backups[0]`; else create `name` (default `"default"`). The `backups[0]` branch ran before the name was ever consulted, so `--name` only mattered on a brand-new account. The GUI relies on a named push creating a distinct backup.

## Goals / Non-Goals

**Goals:**
- A named push with no id creates a new, distinctly-labeled backup.
- Explicit-id and the bare no-id/no-name convenience paths are preserved.
- The resolution logic is unit-testable without a live backend.

**Non-Goals:**
- Enforcing global name uniqueness (names remain labels; id is the key).
- Any change to the chunked upload pipeline, crypto, or version semantics.
- Renaming/merging existing backups.

## Decisions

**Order the checks id → name → list.** Move the `--name` create ahead of the `backups[0]` fallback so a named push always creates. Rationale: matches the documented `[--name <label>]` intent and unblocks per-profile hosting. Alternative (a new `--create` flag) rejected as extra surface; `--name` already signals intent and callers wanting to target an existing backup pass `--backup-id`.

**Extract `backupResolverStore` interface.** `resolveBackupID` takes a 2-method interface (`ListBackups`, `CreateBackup`) instead of the concrete `Dependencies`/`*storage.Client`, satisfied by `*storage.Client` and faked in tests. Pure testability refactor, no behavior change.

## Risks / Trade-offs

- [A user scripting `backup push --name X` repeatedly now gets many backups instead of versions] → Mitigation: documented; to add a version, pass `--backup-id`. The GUI records and reuses ids, so it versions correctly.
- [Consumer skew: GUI shipping before this lands] → Mitigation: GUI gates on the engine pin; engine ships first.

## Migration Plan

Backward-compatible. No data migration. Release normally; the GUI pin auto-bumps via `engine-drift-check`. Rollback = revert; only the no-id+named path reverts to the old (buggy) behavior.
