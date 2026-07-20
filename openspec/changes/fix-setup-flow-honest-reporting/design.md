## Context

The setup flow spans two repos with a JSON envelope between them. Verification on a real machine found three independent defects, all of the same kind: **a surface asserts an outcome nobody verified**. A dry run renders as "Setup complete"; a catalog match renders as "settings this profile carries"; envelope counts render beside an unreconciled event list.

The engine's apply path is correct and stays correct — confirmed end-to-end (`--dry-run` → real install → independent `winget list` and binary check → idempotent re-run reporting `already_installed`). Nothing in this design changes install, restore, or verify behavior.

Current state at the seam, established by reading the contract, the engine struct, and the GUI types together:

| Layer | apply result fields | Status |
|---|---|---|
| `docs/contracts/cli-json-contract.md` | `summary` + `actions` | authoritative |
| Engine `ApplyResult` (`apply.go:258`) | `summary` + `actions` | conforms |
| OpenSpec `apply-restore-envelope` | references `items[]` | stale |
| GUI `EndstateApplyData` (`types.ts:400`) | reads `counts` + `items` | wrong |

`counts` is a `capture` field (contract line 286); `items` is a `generations` field (contract line 781). Neither has ever existed for `apply`. So `envelopeData.items` is permanently `[]`, `reconcileLiveActivity` is permanently a no-op, and the results list is always raw live-stream state — which is how a plan-phase `to_install` row survives into a finished run. The counts happen to be right only because an `else if` branch recomputes them from `actions[]`.

## Goals / Non-Goals

**Goals:**
- The default action installs, and any run that changed nothing says so.
- `restoreModulesAvailable` describes the profile, not the catalog.
- The GUI reads only fields the apply contract defines, and reconciles final state against them.
- Legacy profiles captured before current metadata existed keep working.

**Non-Goals:**
- No change to install, restore, or verify semantics.
- No schema version bump — see Decision 4.
- Not redesigning the settings picker UX; it is fixed by making its data honest.
- Not fixing capture-side module metadata; this change reads what capture already writes.

## Decisions

### Decision 1: Scope `restoreModulesAvailable` by resolved restore entries, with a three-tier fallback

The field is built from `modules.MatchModulesForApps(catalog, mf.Apps)` (`apply.go:393`), which answers "which catalog modules match these apps" — a question nobody asked. It must answer "which modules would actually restore something".

The obvious key is `RestoreEntry.FromModule` (`manifest/types.go:344`). **Empirical finding that rules out using it alone:** on the real `hugo-desktop` profile, `fromModule` is absent from all 20 restore entries — it was captured at `endstateVersion 0.1.0`, before the field existed. Scoping on it alone would return zero modules for legacy profiles, which is worse than the bug.

Resolution order per restore entry:
1. `FromModule` when non-empty — authoritative, written by capture and bundle rewrite.
2. Otherwise derive from the `source` path prefix: `./configs/<id>/…` or `./payload/apps/<id>/…`.
3. If neither resolves for any entry, fall back to the manifest's declared `configModules[]`.

Verified on the legacy profile: tiers 2 and 3 each independently produce exactly the correct 8 modules (sources are uniformly `./configs/<module>/…`, and `configModules` lists the same 8). Measured effect: 41 offered → 8, removing 33 phantom entries.

*Alternative rejected:* stat the filesystem for `configs/<id>/`. Correct but adds I/O to a hot path, and fails for bundles not yet extracted. Path derivation gets the same answer from data already in memory.

*Alternative rejected:* intersect with `configModules[]` only. Simpler, but `configModules` is a declaration while `restore[]` is what executes; they can drift, and only the latter predicts what a restore would do.

### Decision 2: `entryCount` is derived, not stored

`entryCount` = number of restore entries resolved to that module by Decision 1. It costs nothing extra, cannot drift from the scoping it accompanies, and gives the GUI the signal that would have made the phantoms obvious. A module resolving to zero entries is excluded by Decision 1 rather than reported as `entryCount: 0` — the two rules stay consistent by construction.

### Decision 3: Fix the GUI to the contract, not the contract to the GUI

The GUI is the outlier and `cli-source-of-truth` already binds it ("the GUI SHALL present data produced by the CLI without transforming its semantics"). So the GUI drops `counts`/`items`, treats `summary` + `actions` as canonical, and reconciles against `actions[]`.

*Alternative rejected:* add `items[]`/`counts` to the apply envelope. That would ratify a field name collision — `items` already means something different for `generations` — and expand the contract to accommodate a consumer bug.

The stale `items[]` wording in `apply-restore-envelope` is corrected in the same pass, since leaving it is what makes the GUI's reading look defensible.

### Decision 4: Additive envelope change, no schema bump

`entryCount` is additive; older GUIs ignore unknown fields. Narrowing `restoreModulesAvailable` changes contents, not shape. Per `schema-versioning`, neither warrants a bump. The GUI half ships behind the existing engine version gate.

### Decision 5: Dry-run disclosure is a consumer obligation, stated in the contract

The engine already emits `dryRun` correctly; nothing engine-side is missing. The defect is that no consumer reads it. Rather than special-case the GUI, `gui-integration-contract.md` gains the obligation: a consumer presenting apply results SHALL distinguish a dry run and SHALL NOT report completion or installs for one. That makes the GUI fix a conformance fix and binds future consumers.

## Risks / Trade-offs

- **Flipping `dryRunEnabled` to `false` means the default action now mutates the machine.** → This is the intended product behavior and what users already believe is happening. The preview step remains the safe inspection path, and dry run stays available as an explicit opt-in. Users who set it deliberately are untouched; only the shipped default changes.
- **Path-prefix derivation (tier 2) is a heuristic over a naming convention.** → It is third-line only for profiles lacking `FromModule`, is validated against the module catalog before being trusted, and falls through to `configModules[]` when it yields nothing. Verified against a real legacy profile.
- **A profile whose restore entries resolve to no known module would now offer zero settings where it previously offered many.** → Correct behavior — those entries could not have restored anything — but it is a visible drop. Emit a warning when scoping removes every module so it is diagnosable rather than silent.
- **The GUI and engine changes must land together or the settings picker shows counts it cannot use.** → Additive field, ignored by older GUIs; the GUI reads it behind the existing version gate. Either order is safe.
- **Cross-repo change with tests in two suites.** → Engine tests assert the envelope shape; GUI tests assert rendering against fixtures built from real captured envelopes, not hand-written ones. Hand-written fixtures are what let the `counts`/`items` drift survive this long.

## Migration Plan

1. Engine: scoping + `entryCount`, with tests covering all three resolution tiers and the legacy no-`FromModule` profile.
2. Contracts: `cli-json-contract.md` (`entryCount`, and an explicit note that `counts`/`items` are not apply fields); `gui-integration-contract.md` (dry-run disclosure).
3. Specs: correct `apply-restore-envelope`'s `items[]` → `actions[]`.
4. GUI: contract-conformant field reading + reconciliation against `actions[]`; stop dropping `name`/`driver`; real `entryCount`; dry-run in the results header; default flipped.
5. Verify end-to-end on a real profile — the check that found all of this and the only one that would have.

Rollback: engine and GUI changes are independent and separately revertible. The default flip is a one-line revert.

## Open Questions

- Should an explicit dry run reached from the primary action land on a distinct "Preview complete" screen, or the same screen with a banner? Affects copy only, not this design.
- Does any shipped bundle rely on `restoreModulesAvailable` listing catalog matches rather than profile contents? Nothing in-repo does; worth a grep across consumers before merge.
