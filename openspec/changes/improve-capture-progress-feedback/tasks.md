## 1. Lock the engine contracts with failing tests

- [x] 1.1 Add emitter tests for schema-v1 progress serialization, disabled-emitter behavior, and required base fields.
- [x] 1.2 Add capture tests for phase/progress/item/artifact/summary ordering, applicable stage omission, and `present`/`detected` item status across capture backends.
- [x] 1.3 Add bundle tests proving each matched module is collected once and the report matches files, registry entries, warnings, skips, and sensitive exclusions.
- [x] 1.4 Add failure-path tests proving a partial bundle report survives archive publication failure and the JSONC fallback remains authoritative.
- [x] 1.5 Add source-lifecycle tests for default dual-source capture, explicit Store exclusion, source preservation, source-aware planning/batching, Store warning behavior, generation persistence, and source-scoped apply/verify/uninstall commands.

## 2. Add capture progress events

- [x] 2.1 Add the progress event structure and `EmitProgress` support in `go-engine/internal/events` without changing schema version.
- [x] 2.2 Emit `inventory` before package enumeration in Windows and non-Windows capture paths.
- [x] 2.3 Emit `settings` only at the real matched-module collection boundary.
- [x] 2.4 Emit `packaging` through a decoupled bundle-stage callback before archive creation or atomic artifact publication.

## 3. Make bundle collection single-pass

- [x] 3.1 Add `BundleReport` and per-module collection results with paths, counts, statuses, warnings/errors, and sensitive-exclusion totals.
- [x] 3.2 Add `CreateBundleWithReport` while retaining `CreateBundle` as a compatibility wrapper for existing callers.
- [x] 3.3 Return the populated report alongside later manifest, ZIP-write, or rename failures.
- [x] 3.4 Translate the bundle report into capture-envelope config metadata and remove the duplicate `buildConfigModuleResults` collection pass.

## 4. Close the Microsoft Store lifecycle gap

- [x] 4.1 Add source-scoped Winget export/list helpers and enumerate `winget` plus `msstore` concurrently by default.
- [x] 4.2 Add additive manifest app `source` support, preserve `msstore` through capture output, and validate supported Winget source values.
- [x] 4.3 Introduce source-aware package coordinates, partition Winget detection batches by source, and route installation, verification, and best-effort uninstall through the preserved source.
- [x] 4.4 Keep `--include-store-apps` as a deprecated compatibility no-op, add `--exclude-store-apps`, and update usage/capabilities metadata.
- [x] 4.5 Preserve either usable source inventory when the other fails, emit `store_source_unavailable` or `winget_source_unavailable` as applicable, and fail when no selected source yields a usable inventory.
- [x] 4.6 Add the warning `source` field and emit stable once-per-source availability warnings without treating successful-empty source output as unavailable.
- [x] 4.7 Apply deterministic exact-ref source precedence and retain existing different-ref duplicate warnings.
- [x] 4.8 Omit Store version pins and emit one aggregate `store_version_unpinned` warning when `--pin` is requested.
- [x] 4.9 Add source-aware added/removed package records to provisioning generations, retain legacy ref arrays, and migrate rollback to prefer source-aware history.

## 5. Restore canonical item semantics

- [x] 5.1 Replace capture package item status `captured` with `present` plus reason `detected` in every capture backend.
- [x] 5.2 Confirm runtime filtering and `--include-runtimes` behavior remain unchanged across both WinGet sources.

## 6. Update contracts and paired fixtures

- [x] 6.1 Update `docs/contracts/event-contract.md` with the additive progress event and capture stage ordering.
- [x] 6.2 Update manifest/CLI/generation contracts for the additive app source field, source-aware package history, default Store inclusion, compatibility include flag, explicit exclude flag, and warning shapes/codes.
- [x] 6.3 Correct capture item examples in CLI/event contract documentation where needed.
- [x] 6.4 Update the paired GUI/bridge fixture to exercise delayed dual-source inventory, an `msstore` app, and Store-unavailable warning.

## 7. Verification

- [x] 7.1 Run targeted Go tests for events, bundle, snapshot, Winget driver, manifest, capture, apply, verify, and rollback packages.
- [x] 7.2 Run event/manifest contract tests and OpenSpec strict validation.
- [x] 7.3 Perform an independent reviewer/verifier pass focused on source preservation, partial-source warnings, event compatibility, single-pass metadata fidelity, and dirty-worktree overlap.
