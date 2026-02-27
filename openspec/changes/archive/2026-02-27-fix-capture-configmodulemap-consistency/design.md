## Context

The capture command's JSON envelope includes a `configModuleMap` field for the GUI, but it's always empty (`{}`). Apply and verify correctly populate this map by reading `configModules` from the parsed manifest and passing them to `Build-ConfigModuleMap`. The capture block instead gates on `$captureResult.BundleConfigModules`, which is only set for bundle captures and is never populated for the common non-bundle path.

The existing apply/verify pattern (in `bin/endstate.ps1` lines ~4115-4127 and ~4277-4289) reads the manifest with `Read-JsoncFile` and passes `$parsedManifest.configModules` to `Build-ConfigModuleMap`. The capture block should use the same approach as a fallback.

## Goals / Non-Goals

**Goals:**
- Populate `configModuleMap` in capture JSON output when `BundleConfigModules` is empty, using the output manifest's `configModules` array
- Match the apply/verify pattern for consistency (INV-CONFIGMAP-3)
- Ensure `Read-JsoncFile` is available in the capture code path

**Non-Goals:**
- Changing apply or verify behavior (already correct)
- Modifying `Build-ConfigModuleMap` function
- Changing engine/capture.ps1 (the fix is in the JSON envelope construction in bin/endstate.ps1)

## Decisions

**Decision 1: Fallback to output manifest instead of changing engine/capture.ps1**

The capture result object (`$captureResult`) has an `OutputPath` property pointing to the generated manifest. Rather than threading configModules through capture internals, we read the output manifest in the JSON envelope block — same pattern as apply/verify. This is simpler and keeps the fix localized.

Alternative considered: Adding configModules to `$captureResult` in engine/capture.ps1. Rejected because it requires changes to the capture engine for something that's purely a presentation concern (JSON envelope).

**Decision 2: Preserve BundleConfigModules as primary source**

Keep the existing `BundleConfigModules` check as the primary path. Add the manifest-read as an `elseif` fallback. This preserves backward compatibility for bundle captures while fixing non-bundle captures.

**Decision 3: Read-JsoncFile availability**

`Read-JsoncFile` is defined in `engine/manifest.ps1`. In the capture block, `Resolve-EngineScript` is already used to load `config-modules.ps1`. We'll verify that `Read-JsoncFile` is already available from earlier sourcing in the capture path; if not, we'll source `manifest.ps1` before the configModuleMap block.

## Risks / Trade-offs

- **[Risk] Output manifest doesn't exist yet when JSON envelope is built** → The `$captureResult.OutputPath` is populated after capture completes, and the envelope is built after capture returns, so the file exists. Added `Test-Path` guard as defense.
- **[Risk] Read-JsoncFile not sourced in capture path** → Verify at implementation time; source manifest.ps1 if needed. Low risk since manifest loading is used throughout the entrypoint.
