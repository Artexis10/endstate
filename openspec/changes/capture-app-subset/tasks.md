# Tasks: capture-app-subset

## 1. Selective module matching

- [x] 1.1 `modules.MatchModulesForAppsSelective` — ref-only matching; `MatchModulesForApps`
      becomes a wrapper over a shared core gated on `includePathExists`
- [x] 1.2 Tests: selective variant ignores pathExists; legacy variant still includes it
      (proves the change is additive); chocolatey refs still match; no ref match yields nothing

## 2. Capture selection

- [x] 2.1 `CaptureFlags.Only`
- [x] 2.2 `captureSelection`, `parseCaptureOnly` (namespaced token grammar),
      `validateCaptureOnly` (four rejection paths, all pre-write)
- [x] 2.3 Filter placement: after `assignDeterministicCaptureIDs`, before the `--update` merge
- [x] 2.4 Counts: deselected apps increment `skipped`; `totalFound` stays pre-filter
- [x] 2.5 `capture_realizer.go` — same filter, placed after both lanes contribute

## 3. Module scoping

- [x] 3.1 `scopeCatalogToSelection` in `capture_config.go` — narrows the catalog before planning
      so both module tiers are constrained from one place
- [x] 3.2 `captureSelectionError` so a mistyped token keeps `MANIFEST_VALIDATION_ERROR` through
      the finalize path's plain-error return
- [x] 3.3 Tests: pathExists-only module excluded under a selection; named module included;
      unknown module id rejected with the right code

## 4. CLI surface (PROTECTED — modified under explicit instruction)

- [x] 4.1 `main.go` — route `p.only` to capture and rebuild; `onlyMissingValue` guard for both;
      help text for capture and rebuild
- [x] 4.2 `capabilities.go` — `--only` on capture and rebuild; drop phantom `--filter` from
      restore
- [x] 4.3 `rebuild.go` — `RebuildFlags.Only` + propagation via `rebuildApplyFlags`
- [x] 4.4 `docs/contracts/cli-json-contract.md` — Capture-Subset Selection section, capability
      flag lists, `rebuild --only` note, appsIncluded-id caveat

## 5. Verification

- [x] 5.1 `go build ./...` and `go vet ./...` clean
- [x] 5.2 Full suite `go test ./...` — 0 failures
- [x] 5.3 All touched files gofmt-clean and LF (a Python rewrite had introduced CRLF, which
      would have made every diff whole-file)
- [x] 5.4 `npm run openspec:validate`
- [x] 5.5 End-to-end on a real machine (103 apps, 357-module catalog): baseline capture
      attached 46 config modules; `capture --only 7zip-7zip` attached 1 (`apps.7zip`). Bundle
      `configs/` held only `7zip/`, manifest held only `7zip-7zip`, and
      `totalFound == included + skipped` (103 = 1 + 102).

## 6. Follow-ups (not this change)

- [ ] 6.1 `appsIncluded[].id` is the package ref while `--only` matches the manifest app id, so
      capture output does not show the selectable token. Close before surfacing the flag in a UI.
- [ ] 6.2 Bundle restore entries still carry no `fromModule`, so `--restore-filter` remains a
      silent no-op on bundles. Re-verify against the current `config_restore_execution` session
      layer before designing a fix — the restore path was rearchitected.
- [ ] 6.3 Dead flags `--discover` and `--minimize` are parsed, stored, and never read, yet
      advertised in capabilities and the contract. Reclassify as documented deprecated no-ops
      (the repo already has that pattern for `--include-store-apps`) rather than removing them
      from a contract with a live GUI consumer.
- [ ] 6.4 Consider `counts.notSelected` if the GUI needs to distinguish engine-filtered from
      user-deselected apps.
