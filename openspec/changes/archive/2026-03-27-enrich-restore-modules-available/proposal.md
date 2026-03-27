## Why

The GUI has no reliable way to resolve human-readable display names for entries in `restoreModulesAvailable`. It currently cross-references `configModuleMap` -> winget ID -> app event name, but this chain breaks for apps without winget events (e.g. mpv, which matches via `pathExists` only). The engine already has the module catalog loaded when building the dry-run response, so display names can be resolved at the source.

## What Changes

- Change `restoreModulesAvailable` from `string[]` (module IDs) to `[]{ id: string, displayName: string }` (enriched objects)
- Add `RestoreModuleRef` struct to the Go engine's apply command types
- Add `resolveModuleDisplayName` helper with fallback chain: module `displayName` field -> short ID (strip `apps.` prefix)
- Wire apply.go to use the mockable `resolveRepoRootFn` (consistent with capture.go) for testability

## Capabilities

### New Capabilities
- `restore-modules-display-name`: Defines the enriched shape of `restoreModulesAvailable` with display name resolution rules and invariants

### Modified Capabilities
- `restore-filter`: The `restoreModulesAvailable` field changes from `string[]` to `[]{ id, displayName }` — scenarios referencing this field need a delta spec to reflect the new shape

## Impact

- **Go engine**: `go-engine/internal/commands/apply.go` — `ApplyResult` struct, module-matching loop, new helper function
- **JSON contract**: `restoreModulesAvailable` field shape changes from string array to object array (additive — no schema version bump)
- **GUI**: Will need a separate follow-up change to consume the new shape (not in scope)
- **Tests**: New unit tests for display name resolution and enriched output; existing tests unaffected (field is omitempty and only populated when catalog is mocked)
