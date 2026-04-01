## Why

Some Windows applications (FastRawViewer, others) store their configuration in the Windows Registry rather than in files on disk. The current restore strategies (copy, merge-json, merge-ini, append) are all file-based. This means registry-configured apps cannot have their settings backed up, restored, or captured by Endstate, blocking proper config management for a meaningful category of Windows software.

The FastRawViewer module currently references nonexistent file paths because its config actually lives in registry keys under `HKCU\Software\LibRaw LLC\FastRawViewer`. Without a registry-aware restore type, this module cannot function correctly.

## What Changes

- Add a `registry-import` restore type that imports `.reg` files into the Windows Registry via `reg import` and backs up existing keys via `reg export` before overwriting
- Add a `registryKeys` field to the capture definition so modules can declare registry keys to export during capture
- Rewrite the `fastrawviewer` module to use registry paths and the new `registry-import` restore type instead of nonexistent file-based paths
- Add registry-import awareness to the revert system so that reverting a registry restore imports the backup `.reg` file rather than attempting a file copy

## Capabilities

### New Capabilities

- `registry-import-restore`: Engine supports importing `.reg` files into the Windows Registry as a restore strategy, with backup-before-overwrite support
- `registry-capture`: Module capture system supports exporting registry keys to `.reg` files via a `registryKeys` field in the capture definition

### Modified Capabilities

- `restore-dispatch`: The `RunRestore` switch gains a `registry-import` case alongside existing `copy`, `merge-json`, `merge-ini`, and `append` cases
- `revert-dispatch`: The `RunRevert` function gains registry-import awareness, importing backup `.reg` files instead of file-copying them

## Impact

- **File:** `go-engine/internal/modules/types.go` -- add `CaptureRegistryKey` type and `registryKeys` field to `CaptureDef`
- **File:** `go-engine/internal/restore/registry_import.go` -- new file implementing `RestoreRegistryImport` function
- **File:** `go-engine/internal/restore/restore.go` -- add `registry-import` case to `RunRestore` switch
- **File:** `go-engine/internal/restore/revert.go` -- add registry-import awareness to `RunRevert`
- **File:** `go-engine/internal/commands/capture.go` -- handle `registryKeys` entries during capture
- **File:** `modules/apps/fastrawviewer/module.jsonc` -- rewrite to use registry-import restore and registryKeys capture
- **Behavior:** Registry-import is Windows-only; non-Windows platforms receive a clear error
- **No schema version bump** -- additive capability, no breaking changes to existing contracts
