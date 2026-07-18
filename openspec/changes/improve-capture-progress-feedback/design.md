## Context

A real Windows capture on July 15 took 22.5 seconds. The first 14.7 seconds were package inventory with no app item event, followed by 7.6 seconds of settings and bundle work. The command currently emits only a coarse `phase: "capture"` event before that gap.

The same path emits item `status: "captured"`, which is outside the event contract, even though existing tests and specs already require `status: "present"`, `reason: "detected"`. It then creates the bundle by collecting files and registry data and calls `buildConfigModuleResults`, which collects the same modules again solely to reconstruct envelope metadata.

The working tree contains in-flight multi-driver work in capture and event files. The implementation must patch those seams narrowly and preserve existing callers of the bundle API.

## Goals / Non-Goals

**Goals:**

- Emit truthful capture progress before long-running operations.
- Keep the event addition backward compatible within schema v1.
- Restore capture item events to their existing canonical status contract.
- Collect settings once and reuse that exact result for the archive and envelope.
- Preserve fallback JSONC output and useful metadata if bundle publication fails.
- Capture both default WinGet sources and preserve Store source identity through restoration.
- Degrade visibly rather than fail the whole capture when only the Store source is unavailable.

**Non-Goals:**

- Completion percentages or duration estimates.
- User-facing progress wording in the engine.
- Third-party WinGet sources beyond `winget` and `msstore`.
- Guaranteeing installation of Store packages blocked by licensing, region, account, or enterprise policy.
- Exact version restoration for Store packages until WinGet exposes a reliable Store version-selection contract.
- Changes to runtime filters or `--include-runtimes`.
- Broad refactoring of the in-flight multi-driver capture work.

## Decisions

### Add a generic stage-only progress event

Schema v1 gains this additive shape:

```json
{
  "version": 1,
  "runId": "capture-...",
  "timestamp": "2026-07-16T12:00:00Z",
  "event": "progress",
  "phase": "capture",
  "stage": "inventory"
}
```

Capture stages are `inventory`, `settings`, and `packaging`. They are monotonically ordered but form an applicable subset: every capture backend emits `inventory`; paths that collect settings emit `settings`; paths that write an artifact emit `packaging`. No current/total fields or messages are emitted.

The opening `phase` event remains first and the summary remains last. Older consumers ignore the new event type. A generic `progress` shape leaves room for other phases later without expanding this change's stage enum beyond capture.

### Emit stages at real work boundaries

- `inventory` is emitted immediately before package-backend enumeration.
- `settings` is emitted immediately before the single matched-module collection pass.
- `packaging` is emitted immediately before archive creation or atomic artifact publication.

The bundle layer accepts a stage callback or invokes a caller-supplied hook; it does not import the events package. This places `packaging` inside the true boundary instead of emitting it after ZIP creation has already finished.

### Return the collection report from bundle creation

Add a result-bearing API such as:

```go
CreateBundleWithReport(..., onStage func(Stage)) (BundleReport, error)
```

Keep `CreateBundle(...) error` as a compatibility wrapper for existing callers. `BundleReport` contains one result per matched module, including module identity, collected paths, entry count, status, warnings/errors, and sensitive-exclusion count, plus the existing bundle metadata. Empty collections remain explicit empty slices.

The report is populated as collection proceeds and is returned even if a later manifest rewrite, ZIP write, or atomic rename fails. Capture translates the report into `CaptureModuleResult`, `configsIncluded`, `configsSkipped`, `configsCaptureErrors`, and `SensitiveExcludedCount`; it does not run collectors again. The JSONC fallback behavior remains unchanged when publication fails.

### Restore the canonical detected-item contract

All capture backends emit detected packages as:

```json
{ "event": "item", "status": "present", "reason": "detected" }
```

The string `captured` remains valid only for config-module status inside the capture envelope, not for streaming package item status.

### Make complete source coverage the engine default

Ordinary Windows capture will enumerate the two default application sources explicitly and concurrently:

- `winget export --source winget`
- `winget export --source msstore`

Corresponding source-scoped list calls provide display-name and version evidence. Explicit calls avoid pulling in `winget-font` or arbitrary configured third-party sources. The single `inventory` progress stage begins before both calls and remains active until the selected-source inventory is complete.

Both GUI and direct CLI capture inherit this default. The existing `--include-store-apps` argument remains accepted as a deprecated no-op so scripts do not break on argument parsing. A new `--exclude-store-apps` argument prevents Store-source access and returns community-source-only behavior for managed environments. Runtime filtering remains independent and unchanged.

### Preserve package source through the lifecycle

Captured Store apps use the existing Winget package driver and add an explicit manifest field:

```json
{
  "id": "microsoft-store-app",
  "driver": "winget",
  "source": "msstore",
  "refs": { "windows": "9NBLGGH4NNS1" }
}
```

The `source` field is additive in manifest schema v1. It is valid for Winget apps with values `winget` or `msstore`. Source resolution is explicit value first, then the existing Store-ID classifier for legacy source-less Store refs, then `winget` as the ordinary backward-compatible default. Capture envelopes report Store apps with `source: "msstore"`.

Winget detection, installation, verification, and best-effort uninstall route by the preserved source and pass `--source msstore` for Store apps. For legacy profiles that contain a known Store package ID without the new field, the existing Store-ID classifier provides a compatibility fallback. New profiles rely on the explicit field rather than requiring the GUI or consumers to infer source.

Internally, planning and batching use a source-aware package coordinate `(driver, source, ref)`. Detection result maps are keyed by that coordinate, not by ref alone, and Winget batches are partitioned by source before invoking the CLI. An explicit normalized manifest source always wins; Store-ID inference is used only when source is absent. This prevents an identically named or identified package in one source from satisfying a request for the other source.

Provisioning generations gain additive source-aware package records, for example `addedPackages: [{ "ref": "9NBLGGH4NNS1", "source": "msstore" }]`, alongside the existing `addedRefs` compatibility array. New Winget applies populate both. Best-effort rollback prefers the source-aware records and falls back to legacy refs plus Store-ID inference for old generations. Rollback generations likewise preserve source-aware removed-package records so audit history remains unambiguous.

Store packages omit version pins even when capture uses `--pin`. If one or more Store packages are affected, the engine emits exactly one aggregate non-fatal `store_version_unpinned` warning whose message includes the affected count. This avoids both writing a promise the Store source cannot reliably restore and flooding the GUI with one warning per app.

### Treat Store-source failure as partial capture

Source enumeration produces independent results. A source result is usable when its command succeeds and its output parses, including a valid empty package list. Source-unavailable warnings are emitted only for command, access, or parse failures—not for successful-empty inventories.

When `winget` returns a non-empty usable inventory and `msstore` fails because the source is unavailable, disabled, or blocked, capture keeps the community-source apps and artifact, and adds warning code `store_source_unavailable`. The inverse partial result also succeeds with warning code `winget_source_unavailable`. Neither case silently claims complete source coverage. The existing non-discovery empty-ledger guard is evaluated after merging all selected usable inventories: an aggregate zero-package result still fails after retry, even if an individual source query succeeded with an empty list.

Capture warning objects use the existing envelope `warnings` array with the additive shape `{ code, message, driver: "winget", source }`. Source-availability warnings occur once per failed source per capture. The aggregate Store-pin warning occurs at most once per capture. Multiple distinct warnings remain separate array entries.

If an exact normalized ref is reported by both default sources, capture retains one entry: Store-format IDs prefer `msstore`; all other IDs prefer `winget`. This deterministic precedence prevents duplicate install attempts. Different refs that merely share a display name remain separate and continue through the existing possible-duplicate warning path.

Capture fails only when no selected package source produces a usable inventory under the existing empty-ledger rules. Package-level Store licensing, region, account, and policy failures during apply remain normal visible item failures; they are not converted to success.

## Risks / Trade-offs

- **[Risk] Report data becomes weaker than the removed metadata pass** → Include paths, counts, warnings/errors, skipped state, and sensitive exclusions in the bundle report and test envelope/report equivalence.
- **[Risk] Archive failure loses collection evidence** → Return the partially populated report alongside the error and retain JSONC fallback output.
- **[Risk] Bundle package becomes coupled to streaming** → Use a small stage callback owned by the caller; do not import `internal/events` into `internal/bundle`.
- **[Risk] Dirty capture/event files are overwritten** → Apply narrow patches against current contents and keep the larger API refactor in currently clean bundle files.
- **[Risk] A backend has no settings work** → Stages are an ordered applicable subset, so omitting `settings` is contract-valid.
- **[Risk] Default capture contents change for existing CLI users** → Document the behavioral change, preserve the old include flag as a no-op, and provide `--exclude-store-apps` for the old source set.
- **[Risk] Store access is blocked on managed Windows devices** → Run sources independently, keep successful community results, and emit `store_source_unavailable`.
- **[Risk] Source identity is lost between capture and apply** → Persist `source: "msstore"` in the manifest and add lifecycle tests for capture, plan, apply, verify, and uninstall routing.
- **[Risk] Source identity is lost before a later rollback** → Persist additive source-aware package records in provisioning generations and retain ref-only fields for old readers.
- **[Risk] Ref-only batching aliases two sources** → Key plans and results by `(driver, source, ref)` and partition Winget batches by source.
- **[Risk] The same installed app matches both catalogs** → Apply deterministic exact-ref source precedence and test that only one manifest/apply entry survives.
- **[Risk] Dual-source enumeration increases latency** → Run source calls concurrently and expose the truthful inventory stage through the paired GUI progress work.

## Migration Plan

1. Add emitter serialization and bundle-report tests before implementation.
2. Introduce the compatible bundle API and migrate capture to its report.
3. Add source-aware Winget enumeration, coordinate-keyed planning, generation persistence, and lifecycle routing, then make the two-source behavior the capture default.
4. Add progress emissions and correct item statuses in each capture backend.
5. Update event, manifest, generation, CLI, and paired GUI contracts and fixtures.
6. Rollback can restore the prior binary. New `source` fields are additive; older engines may ignore them and therefore cannot guarantee Store restoration.

## Open Questions

None.
