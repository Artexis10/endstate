## 1. Schema (types)

- [x] 1.1 `modules/types.go`: add `Key`/`ValueName`/`ValueType`/`Data` to `RestoreDef`; `ValueType`/`Data` to `VerifyDef`; add `CaptureRegistryValue` + `registryValues` to `CaptureDef`
- [x] 1.2 `manifest/types.go`: mirror value-level fields on `RestoreEntry`/`VerifyEntry`
- [x] 1.3 `restore/restore.go`: add value-level fields to `RestoreAction`

## 2. registry-set restore primitive

- [x] 2.1 `restore/registry_set.go` (cross-platform): validation (HKCU-only, value name, supported type), prior-value backup sidecar read/write
- [x] 2.2 `restore/registry_set_windows.go`: `RestoreRegistrySet` (probe → idempotent skip → backup-before-write → create-key → write); numeric DWORD equality; revert helper (restore-or-delete)
- [x] 2.3 `restore/registry_set_other.go`: non-Windows stub (validation still runs; reports Windows-only)
- [x] 2.4 `restore/restore.go`: `registry-set` early dispatch + ID generation
- [x] 2.5 `restore/revert.go`: `registry-set` revert branch (restore prior value, or delete if absent)

## 3. registry-value-equals verify

- [x] 3.1 `verifier/registry_windows.go`: `CheckRegistryValueEquals` (DATA comparison, optional type assertion, numeric DWORD)
- [x] 3.2 `verifier/registry_other.go`: non-Windows stub
- [x] 3.3 `verifier/verifier.go`: dispatch `registry-value-equals`

## 4. registryValues capture

- [x] 4.1 `bundle/collect.go`: `CollectRegistryValues` (value-level read → JSON snapshot)
- [x] 4.2 `bundle/create.go` + `commands/capture.go`: wire `CollectRegistryValues` into the capture flow; carry value-level fields through the bundle restore-entry rewriter

## 5. Wiring

- [x] 5.1 `modules/expander.go`: carry value-level fields in the expand copy loop
- [x] 5.2 `commands/restore.go`: carry value-level fields in `convertToActions` + `registry-set` ID

## 6. Seed modules

- [x] 6.1 `modules/windows-settings/personalization/module.jsonc` — dark mode (AppsUseLightTheme=0, SystemUsesLightTheme=0)
- [x] 6.2 `modules/windows-settings/explorer/module.jsonc` — show extensions (HideFileExt=0) + hidden (Hidden=1)
- [x] 6.3 `modules/windows-settings/taskbar/module.jsonc` — left alignment (TaskbarAl=0)

## 7. Tests (hermetic, Windows-native; scratch key cleaned up)

- [x] 7.1 `restore/registry_set_test.go` — HKCU rejection, set+backup, dry-run, idempotent (incl. 0x-hex), unsupported type
- [x] 7.2 `restore/registry_set_revert_test.go` — revert deletes created value; revert restores prior data
- [x] 7.3 `verifier/registry_value_equals_windows_test.go` — match/mismatch/type-mismatch/missing/string + RunVerify dispatch
- [x] 7.4 `bundle/collect_registry_values_windows_test.go` — value-level snapshot; required-missing error
- [x] 7.5 `modules/expander_test.go` — value-level fields carry through expand
- [x] 7.6 `modules/windows_settings_catalog_test.go` — windows-settings category loads; restore/verify parity

## 8. Verification

- [x] 8.1 `cd go-engine && go test ./internal/restore/... ./internal/verifier/... ./internal/bundle/... ./internal/modules/...` green
- [x] 8.2 `GOOS=linux go build ./...` green (stubs compile)
- [x] 8.3 `npm run openspec:validate` (strict) passes for this change

## 9. Contract documentation (PROTECTED — PENDING USER APPROVAL)

- [ ] 9.1 `docs/contracts/restore-safety-contract.md` — add the backup-and-overwrite exception (wording in `design.md`). **NOT edited by this change; awaiting explicit go-ahead.**
