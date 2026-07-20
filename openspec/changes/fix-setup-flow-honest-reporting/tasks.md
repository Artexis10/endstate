## 1. Engine: scope restoreModulesAvailable to profile contents

- [x] 1.1 Add a module-resolution helper in `go-engine/internal/commands/` implementing the three-tier order from design Decision 1: `fromModule` → `source` path prefix (`./configs/<id>/`, `./payload/apps/<id>/`) validated against the loaded catalog → declared `configModules[]`
- [x] 1.2 Add `EntryCount` to `RestoreModuleRef` (`apply.go:~266`) with json tag `entryCount`
- [x] 1.3 Replace the `restoreModulesAvailable` construction at `apply.go:393` so membership comes from resolved restore entries rather than `MatchModulesForApps` alone; keep `configModuleMap`/`packageModuleMap` construction unchanged
- [x] 1.4 Derive `entryCount` from the same resolution pass so membership and count cannot disagree; exclude zero-count modules rather than emitting `entryCount: 0`
- [x] 1.5 Preserve deterministic ordering by module ID (existing `restore-modules-display-name` requirement)
- [x] 1.6 Emit a `CommandWarning` when the manifest has restore entries but none resolves to a catalog module
- [x] 1.7 Thread the changed `restoreModulesAvailable` through the other apply lanes: `apply_brew_only.go`, `apply_realizer.go`, `apply_generation.go`

## 2. Engine: tests

- [x] 2.1 Unit test tier 1 — entries with `fromModule` resolve to it, taking precedence over a conflicting source path
- [x] 2.2 Unit test tier 2 — legacy entries with no `fromModule` and `./configs/<id>/` sources resolve via the catalog; a derived ID absent from the catalog does not resolve
- [x] 2.3 Unit test tier 3 — fallback to declared `configModules[]` when no entry resolves by tier 1 or 2
- [x] 2.4 Regression test the measured case: a manifest whose app list matches many catalog modules but which carries restore payload for only a few yields only the few (the 41→8 case)
- [x] 2.5 Test that a manifest with empty `configModules` and empty `restore` omits `restoreModulesAvailable` entirely (the `configModules: []` profile that returned 16)
- [x] 2.6 Test `entryCount` correctness and the never-zero invariant
- [x] 2.7 Test the warning fires when restore entries cannot be attributed
- [x] 2.8 Run `cd go-engine && go test ./internal/commands/...`
- [x] 2.9 Preserve `--only` scoping: `mf.Restore` is not filtered by `--only` (only `mf.Apps` is), so scoping from restore entries alone let offered settings escape the subset. Caught by the existing `TestRunApply_Only_RestoreScopeFollowsSubset`; fixed by passing the modules matched to the filtered app set as an allow-list
- [x] 2.10 Test that a `--only` subset excluding every module with payload yields nothing but does NOT emit the unattributed warning — the two conditions are distinct
- [x] 2.11 Run the full engine suite (`go test ./...`): 34 packages ok, 0 failures

## 3. Contracts

- [x] 3.1 `docs/contracts/cli-json-contract.md` — document `entryCount` in the apply `restoreModulesAvailable` shape
- [x] 3.2 `docs/contracts/cli-json-contract.md` — state explicitly that the apply envelope carries `summary` + `actions`, and that `counts` (capture) and `items` (generations) are not apply fields
- [x] 3.3 `docs/contracts/gui-integration-contract.md` — add the dry-run disclosure obligation for consumers presenting apply results
- [x] 3.4 Confirm no schema version bump is required per `schema-versioning` (additive field, narrowed array contents)

## 4. GUI: contract-conformant result reading

- [ ] 4.1 `src/types.ts` — remove `counts` and `items` from `EndstateApplyData`; drop the "legacy" annotation on `summary`; add `entryCount` to the restore-module entry type
- [ ] 4.2 `src/App.tsx:~2296` — derive counts from `summary`/`actions[]` only; remove the `envelopeData.counts` branch
- [ ] 4.3 `src/lib/apply-utils.ts` — reconcile live events against `actions[]` instead of the never-present `items[]`
- [ ] 4.4 `src/lib/apply-utils.ts:~1027` — stop dropping `name` and `driver` when rebuilding events during reconciliation
- [ ] 4.5 Verify no item can render with a non-terminal status (`to_install`/`installing`) after a completed run

## 5. GUI: dry-run disclosure and default

- [ ] 5.1 `src/settings.ts:42` — flip `DEFAULT_SETTINGS.dryRunEnabled` to `false`
- [ ] 5.2 Confirm the settings migration path leaves an explicit user-set `true` intact (only the default changes)
- [ ] 5.3 Plumb `EndstateApplyData.dryRun` through `ApplyResult` into `setup-flow.tsx`
- [ ] 5.4 `setup-flow.tsx:~1102` — replace the hardcoded "Setup complete" with dry-run-aware copy stating nothing was installed
- [ ] 5.5 Resolve the open question on preview copy (distinct screen vs banner) — copy only, no structural change

## 6. GUI: honest settings picker

- [ ] 6.1 `setup-flow.tsx:~985` — pass the engine's real `entryCount` instead of the hardcoded `entries: 0`
- [ ] 6.2 Confirm `config-module-selector.tsx:79` now renders per-module counts, since the `entries > 0` guard is finally satisfiable
- [ ] 6.3 Surface a signal when "apps and settings" is selected with no modules checked, so the apply does not silently degrade to apps-only
- [ ] 6.4 Fix the scope-blind preview: either pass restore scope to the preview call (`App.tsx:~1911`) or stop force-resetting the toggle on preview (`setup-flow.tsx:309`) so preview and apply agree

## 7. GUI: tests and closing the mock/engine gap

The E2E suite could not have caught any of these defects. 30 of 31 specs run
against the `__ENDSTATE_MOCK_ENGINE__` fixture and none spawns a real binary,
and the mock emits `items`/`counts` while emitting no `actions`, `summary`,
`dryRun`, or `restoreModulesAvailable` at all. The mock was written to satisfy
the GUI's types rather than to mirror the engine, so CI verifies only that the
GUI agrees with itself. Fixing the mock matters more than any single defect here
— without it the same class of drift returns silently.

- [ ] 7.1 Rebuild apply-envelope test fixtures from real captured engine output rather than hand-written objects — hand-written fixtures are what let the `counts`/`items` drift survive
- [ ] 7.6 Correct `src/e2e/mock-engine.ts` to emit the real apply envelope (`dryRun`, `summary`, `actions`, `restoreModulesAvailable`, `warnings`) and stop emitting `items`/`counts` for apply
- [ ] 7.7 Add a contract-conformance test that asserts the mock's apply envelope shape matches a fixture captured from the real engine, so the mock cannot drift from the producer again
- [ ] 7.8 Add at least one CI job that runs the real engine binary end-to-end against a temp profile (install a tiny package, assert `installed` + idempotent re-run), so the producer is in the circuit at least once
- [ ] 7.2 Test that a `dryRun: true` envelope never renders completion copy
- [ ] 7.3 Test reconciliation against `actions[]`, including that `name` survives and no `to_install` row remains after completion
- [ ] 7.4 Test the module picker renders real entry counts
- [ ] 7.5 Run the GUI test suite

## 8. End-to-end verification

- [ ] 8.1 Build the engine and re-bootstrap (`endstate bootstrap`) so the GUI runs the new binary, per the stale-bootstrapped-copy landmine
- [ ] 8.2 Verify against a real legacy profile (no `fromModule`) that the settings picker offers only modules the profile carries, with correct counts
- [ ] 8.3 Verify a real apply from the GUI installs an app that is genuinely absent, and that the results screen reports it as installed with its friendly name
- [ ] 8.4 Verify a dry run from the GUI reports that nothing was installed
- [ ] 8.5 Re-run the apply to confirm idempotence reporting (`present` / `already_installed`)
- [ ] 8.6 `npm run openspec:validate`
