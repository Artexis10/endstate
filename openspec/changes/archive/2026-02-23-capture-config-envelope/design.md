## Approach

Surface structured config module metadata through the existing capture pipeline by enriching return values at each layer. All data already exists internally — this is a plumbing change.

## Data Flow

```
Get-MatchedConfigModulesForApps → $matchedModules (has id, displayName, matches.winget)
    ↓
Invoke-CollectConfigFiles → $configResult (has included/skipped/errors + NEW moduleFileCounts)
    ↓
New-CaptureBundle → builds ConfigModulesDetail[] from both, adds to result
    ↓
Invoke-CaptureCore (bin/endstate.ps1) → wires BundleConfigModulesDetail to result
    ↓
JSON envelope handler → surfaces as data.configModules[]
```

## ConfigModulesDetail Schema

Each entry in the array:

```
{
  id: string          # e.g. "apps.vscode"
  appId: string       # e.g. "vscode" (id with "apps." prefix stripped)
  displayName: string # e.g. "Visual Studio Code"
  status: string      # "captured" | "skipped" | "error"
  filesCaptured: int  # count of files captured for this module
  wingetRefs: string[] # module's matches.winget array (may be empty)
}
```

## Changes by File

### engine/bundle.ps1

1. **Invoke-CollectConfigFiles**: Add `moduleFileCounts` hashtable to result, keyed by `$moduleDirName` → file count. Populate alongside existing `$moduleFilesCopied` tracking.

2. **New-CaptureBundle**: After config collection, build `ConfigModulesDetail` array by iterating `$matchedModules` and mapping status from `$configResult`. Add to `$result` hashtable.

### bin/endstate.ps1

3. **Invoke-CaptureCore**: After `New-CaptureBundle` succeeds, wire `$bundleResult.ConfigModulesDetail` to `$result.BundleConfigModulesDetail`.

4. **JSON envelope handler**: When `$captureResult.BundlePath` is set, add `$data.configModules` from `$captureResult.BundleConfigModulesDetail`.

## Backward Compatibility

- Existing `configsIncluded`, `configsSkipped`, `configsCaptureErrors` fields preserved
- `configModules` is additive — per CLI JSON contract, additive fields are backward-compatible
- `configModules` only present when bundle capture was used (same condition as existing config fields)

## Edge Cases

- Module with no `matches.winget` → `wingetRefs` is empty array `[]`
- Module where all files are optional and missing → status "skipped", filesCaptured 0
- No matched modules → `ConfigModulesDetail` is empty array (not null/absent)
- Error during file copy → status "error"
