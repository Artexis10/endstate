## Context

The apply command's dry-run response includes `restoreModulesAvailable` as a flat `[]string` of qualified module IDs (e.g. `["apps.mpv", "apps.vscode"]`). The GUI needs human-readable labels for these modules but currently cross-references through `configModuleMap` -> winget ID -> app event `name` field. This chain fails for modules matched via `pathExists` only (no winget events emitted), leaving the GUI with raw IDs.

The module catalog is already loaded and matched during the apply planning phase. Each `Module` struct has a `DisplayName` field populated from `module.jsonc`. The information is available — it just isn't being passed through.

## Goals / Non-Goals

**Goals:**
- Enrich `restoreModulesAvailable` entries with display names resolved from the module catalog
- Provide a reliable fallback when `displayName` is empty (strip `apps.` prefix)
- Maintain backward compatibility for existing consumers (additive shape change)
- Keep the change testable with existing mock infrastructure

**Non-Goals:**
- Changing the `configModuleMap` shape (separate concern, separate change)
- GUI-side consumption of the new shape (follow-up task in the GUI repo)
- Adding new I/O or catalog loading paths (reuse what's already loaded)
- Schema version bump (additive change per `schema-versioning` spec)

## Decisions

### 1. New `RestoreModuleRef` struct vs inline map

**Decision**: Introduce a `RestoreModuleRef` struct with `id` and `displayName` fields.

**Rationale**: A named struct is self-documenting, type-safe, and extensible if future fields are needed (e.g. `hasRestore`). An inline `map[string]string` would be ambiguous and harder to extend.

### 2. Display name fallback chain

**Decision**: `module.DisplayName` -> `strings.TrimPrefix(mod.ID, "apps.")`.

**Alternative considered**: Adding a `Name` field to `Module`. Rejected — no existing modules use a `name` field; `displayName` is the established convention across all 30+ modules. A two-step fallback is sufficient.

### 3. Wire `resolveRepoRootFn` in apply.go

**Decision**: Replace `config.ResolveRepoRoot()` with the mockable `resolveRepoRootFn` (already defined in `capture.go`) in the catalog-loading block of `RunApply`.

**Rationale**: `capture.go` and its tests already use this pattern. Without it, `withMockCatalog` in tests can't override the repo root, making catalog-dependent apply tests impossible. The restore-phase call at line 368 is left as `config.ResolveRepoRoot()` since it's a different code path not covered by this change.

## Risks / Trade-offs

- **[Shape breaking change for GUI]** -> The GUI currently expects `string[]`. It will need updating to handle objects. Mitigated: this is documented as a follow-up task, and the GUI can check the element type at runtime.
- **[Empty displayName in module.jsonc]** -> Fallback to short ID ensures `displayName` is never empty in the output. Tested explicitly.
- **[No schema version bump]** -> Per `schema-versioning` spec, additive changes don't require a bump. Consumers should tolerate shape changes via `omitempty` patterns.
