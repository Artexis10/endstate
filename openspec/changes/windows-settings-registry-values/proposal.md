## Why

Endstate's per-app config modules cover application settings, but a large slice of the "clean-install tax" is the user's **Windows OS preferences** — dark mode, showing file extensions, taskbar alignment, and so on. These live as individual *named values* inside HKCU registry keys that also hold many unrelated, co-resident values.

The existing `registry-import` restore type is **key-scoped**: it imports a whole `.reg` file, which would clobber co-resident values under the same key (e.g. setting `HideFileExt` by importing the whole `Explorer\Advanced` key would also rewrite `TaskbarAl`, `ShowSuperHidden`, and dozens of others). To safely manage OS preferences we need **value-level** capture/restore/verify primitives that touch exactly one named value and nothing else.

## What Changes

- Add a `registry-set` restore type that sets a single named value (`key`, `valueName`, `valueType` ∈ {REG_DWORD, REG_SZ, REG_EXPAND_SZ}, `data`) under an HKCU key. It reuses the existing HKCU-only gate, records the prior value (type+data, or "absent") to `state/backups/<runID>/` **before** writing, is idempotent (skip-if-already-equal), honors dry-run, and creates the key if missing.
- Add a `registryValues` capture mode that reads specific named values (not whole keys) and snapshots them to `configs/<module>/registry-values.json`.
- Add a `registry-value-equals` verify type that compares value **DATA** (and optional type), extending the existing exists-only registry check.
- Add `registry-set` awareness to revert: it restores the exact prior value, or **deletes** the value when it was absent before the write.
- Wire the new value-level fields (`key`/`valueName`/`valueType`/`data`) through `modules/types.go`, the manifest `RestoreEntry`/`VerifyEntry`, the `ExpandConfigModules` copy loop, the bundle restore-entry rewriter, and the `RunRestore` dispatch.
- Author seed `modules/windows-settings/<group>/module.jsonc` modules (personalization/dark-mode, explorer/show-extensions+hidden, taskbar/left-alignment), each with `registryValues` capture + `registry-set` restore + `registry-value-equals` verify, fully reversible.

## Capabilities

### New Capabilities

- `registry-set-restore`: Engine supports setting a single named HKCU registry value as a restore strategy, with prior-value backup recorded before overwrite, idempotent skip, dry-run safety, and HKCU-only enforcement.
- `registry-value-capture`: Module capture supports a `registryValues` field that snapshots specific named values (value-level), never reading or rewriting co-resident values.
- `registry-value-verify`: Verify supports a `registry-value-equals` type comparing value DATA (numeric for DWORD, exact for strings), with an optional type assertion.

### Modified Capabilities

- `restore-dispatch`: The `RunRestore` switch gains a `registry-set` case alongside `copy`, `merge-json`, `merge-ini`, `append`, `delete-glob`, and `registry-import`.
- `revert-dispatch`: `RunRevert` gains `registry-set` awareness — restore-prior-value or delete-if-absent.

## Safety / Restore default exception (PENDING USER APPROVAL)

The value-level `registry-set` default is **backup-and-overwrite**: it always records the prior value and then writes the desired value (skipping only when already equal). This differs from file-restore's default of **skip** (file copy is non-destructive and leaves an existing target untouched unless explicitly told otherwise). The rationale: a single named OS-preference value has no meaningful "merge" or "leave alone" semantics — the user is declaring a desired value, and reversibility is guaranteed by the mandatory prior-value backup rather than by skipping.

This exception requires an update to `docs/contracts/restore-safety-contract.md`. That file is **protected and decision-gated**; this change does **not** edit it. The intended wording is captured verbatim in `design.md` and is **PENDING USER APPROVAL** before the contract is touched.

## Impact

- **File:** `go-engine/internal/modules/types.go` — add value-level fields to `RestoreDef`/`VerifyDef`; add `CaptureRegistryValue` + `registryValues` to `CaptureDef`.
- **File:** `go-engine/internal/manifest/types.go` — mirror value-level fields on `RestoreEntry`/`VerifyEntry`.
- **File:** `go-engine/internal/restore/registry_set.go` (new, cross-platform helpers) + `registry_set_windows.go` / `registry_set_other.go` — `RestoreRegistrySet`, prior-value backup, revert helper.
- **File:** `go-engine/internal/restore/restore.go` — add `registry-set` dispatch + ID generation.
- **File:** `go-engine/internal/restore/revert.go` — add `registry-set` revert branch.
- **File:** `go-engine/internal/verifier/registry_windows.go` / `registry_other.go` / `verifier.go` — `CheckRegistryValueEquals` + dispatch.
- **File:** `go-engine/internal/bundle/collect.go` — `CollectRegistryValues`; `create.go` carries value-level fields through the bundle rewriter.
- **File:** `go-engine/internal/commands/{capture,restore}.go` — wire `CollectRegistryValues` into capture; carry value-level fields in `convertToActions`.
- **File:** `go-engine/internal/modules/expander.go` — carry value-level fields in the expand loop.
- **Files:** `modules/windows-settings/{personalization,explorer,taskbar}/module.jsonc` — seed OS-settings modules.
- **Behavior:** Value-level registry ops are HKCU-only and Windows-only; non-Windows receives a clear error. Rides the existing `--enable-restore` opt-in gate (no new CLI flag).
- **No schema version bump** — additive fields, no breaking changes.
- **PROTECTED / PENDING:** `docs/contracts/restore-safety-contract.md` — backup-and-overwrite exception wording flagged for explicit go-ahead; NOT edited by this change.
