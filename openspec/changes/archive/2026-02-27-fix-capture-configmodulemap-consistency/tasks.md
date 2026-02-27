## 1. Investigation

- [x] 1.1 Verify `Read-JsoncFile` is available in the capture JSON envelope code path in `bin/endstate.ps1` (check if `engine/manifest.ps1` is sourced before the capture block)

## 2. Core Implementation

- [x] 2.1 In `bin/endstate.ps1`, replace the capture configModuleMap block with fallback logic: if `BundleConfigModules` is empty, read configModules from the output manifest via `Read-JsoncFile` and pass to `Build-ConfigModuleMap`
- [x] 2.2 If `Read-JsoncFile` is not sourced in the capture path, add a source statement for `engine/manifest.ps1` before the configModuleMap block

## 3. Verification

- [x] 3.1 Test capture with `--profile` flag and `--json`: verify configModuleMap is populated with winget ID keys and module name values
- [x] 3.2 Test capture with `--out` flag and `--json`: verify configModuleMap is populated when output manifest contains configModules
- [x] 3.3 Verify capture still returns empty `{}` when no configModules resolve to winget refs
