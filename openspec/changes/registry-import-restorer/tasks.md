## 1. Module Types

- [x] 1.1 Add `CaptureRegistryKey` type to `go-engine/internal/modules/types.go`
- [x] 1.2 Add `registryKeys` field to `CaptureDef` in `go-engine/internal/modules/types.go`

## 2. Restore Implementation

- [x] 2.1 Create `go-engine/internal/restore/registry_import.go` with `RestoreRegistryImport` function
- [x] 2.2 Add `registry-import` case to `RunRestore` switch in `go-engine/internal/restore/restore.go`
- [x] 2.3 Add HKCU-only validation (reject HKLM targets)

## 3. Revert Implementation

- [x] 3.1 Add registry-import awareness to `revert.go` (based on current dispatch pattern)

## 4. Capture Implementation

- [x] 4.1 Handle `registryKeys` in capture command (add registry export logic to `go-engine/internal/commands/capture.go`)

## 5. Module Rewrite

- [x] 5.1 Rewrite `modules/apps/fastrawviewer/module.jsonc` (remove bogus winget ID, add `pathExists` matchers, replace file-based restore with `registry-import` entries, replace file-based capture with `registryKeys` entries, update verify, update notes)

## 6. Tests

- [x] 6.1 Add unit tests for `RestoreRegistryImport`
- [x] 6.2 Add unit test for HKCU validation (HKLM rejection)

## 7. Verification

- [x] 7.1 Run `go test ./...` to verify no regressions
