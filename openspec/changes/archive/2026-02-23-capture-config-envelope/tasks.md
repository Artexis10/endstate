## Implementation Tasks

### Task 1: Enrich New-CaptureBundle return value (engine/bundle.ps1)

**Files:** `engine/bundle.ps1`

1. In `Invoke-CollectConfigFiles`, add `moduleFileCounts` hashtable to `$result` keyed by `$moduleDirName` → count of files copied for that module.
2. In `New-CaptureBundle`, after config collection, build `ConfigModulesDetail` array from `$matchedModules` + `$configResult`:
   - id, appId (strip "apps." prefix), displayName, status (captured/skipped/error), filesCaptured, wingetRefs
3. Add `ConfigModulesDetail` to the result hashtable.

### Task 2: Surface configModules in capture JSON envelope (bin/endstate.ps1)

**Files:** `bin/endstate.ps1`

1. In `Invoke-CaptureCore`, wire `$bundleResult.ConfigModulesDetail` to `$result.BundleConfigModulesDetail`.
2. In the capture JSON envelope handler, add `$data.configModules` from `$captureResult.BundleConfigModulesDetail` when bundle capture was used.

### Task 3: Update OpenSpec (openspec/specs/capture-config-metadata/spec.md)

**Files:** `openspec/specs/capture-config-metadata/spec.md`

Extend the existing spec with the 5 JSON envelope scenarios from the delta spec.

### Task 4: Add unit tests

**Files:** `tests/unit/Bundle.Tests.ps1`

Add Pester tests for `ConfigModulesDetail` in `New-CaptureBundle` result and `moduleFileCounts` in `Invoke-CollectConfigFiles` result.
