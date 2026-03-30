# exclude-configs Specification

## Purpose
Defines the `excludeConfigs` manifest field behavior for suppressing config module expansion during restore. This applies across both the PowerShell and Go engines, ensuring that specified config modules are skipped during configModules expansion while the associated apps are still installed.

## Requirements

### Requirement: excludeConfigs Suppresses Config Module Expansion

The engine SHALL support an `excludeConfigs` field on the manifest (array of strings). During configModules expansion, any module whose ID appears in `excludeConfigs` SHALL be skipped entirely -- its restore and verify entries SHALL NOT be injected into the manifest. The app associated with the module is still installed normally.

#### Scenario: Module listed in excludeConfigs is skipped during expansion

- **WHEN** a manifest has `configModules: ["apps.vscode", "apps.git"]` and `excludeConfigs: ["apps.vscode"]`
- **THEN** only the `apps.git` module's restore and verify entries are injected
- **AND** the `apps.vscode` module's restore and verify entries are NOT injected

#### Scenario: App is still installed when its config module is excluded

- **WHEN** a manifest excludes a config module via `excludeConfigs`
- **AND** the associated app is listed in the manifest's `apps` array
- **THEN** the app is still installed during apply
- **AND** only its config restoration is suppressed

#### Scenario: Empty or absent excludeConfigs has no effect

- **WHEN** a manifest has no `excludeConfigs` field or an empty array
- **THEN** all configModules are expanded normally (backward compatible)

#### Scenario: excludeConfigs with short and qualified IDs

- **WHEN** `excludeConfigs` contains `"vscode"` (short ID)
- **AND** `configModules` contains `"apps.vscode"` (qualified ID)
- **THEN** the module is excluded because `"vscode"` matches `"apps.vscode"`
- **AND** when `excludeConfigs` contains `"apps.vscode"` (qualified ID), it also matches

#### Scenario: Synthesized apps respect excludeConfigs

- **WHEN** a config module would synthesize a manual app entry (pathExists matcher, no winget ID)
- **AND** the module is listed in `excludeConfigs`
- **THEN** the synthesized app entry is NOT created

#### Scenario: excludeConfigs is preserved through manifest serialization

- **WHEN** a manifest with `excludeConfigs` is loaded and serialized
- **THEN** the `excludeConfigs` array is preserved in the output

## Invariants

### INV-EXCLUDECONFIG-1: Exact and Prefix Matching

- `excludeConfigs` entries match module IDs by exact match or by short-ID-to-qualified-ID expansion
- Short ID `"vscode"` matches qualified `"apps.vscode"` (prepend `"apps."` prefix)
- Qualified ID `"apps.vscode"` matches `"apps.vscode"` exactly
- No wildcard or partial matching is supported

### INV-EXCLUDECONFIG-2: Additive Optional Field

- `excludeConfigs` is an optional field that defaults to an empty array
- No schema version bump is required
- Existing manifests without `excludeConfigs` are valid and unaffected

### INV-EXCLUDECONFIG-3: Install-Restore Separation

- `excludeConfigs` suppresses config restoration only, not app installation
- This reinforces the separation-of-concerns invariant: install and configure are distinct pipeline stages

### INV-EXCLUDECONFIG-4: Cross-Engine Parity

- Both the PowerShell engine and Go engine SHALL honor `excludeConfigs` during configModules expansion
- The matching semantics (exact + short-to-qualified) SHALL be consistent across engines

## Affected Commands
- apply (configModules expansion during manifest loading)
- restore (configModules expansion during manifest loading)
- verify (configModules expansion during manifest loading)

## Implementation
- PowerShell: `engine/config-modules.ps1` (Expand-ConfigModules function)
- Go: `go-engine/internal/modules/expander.go` (ExpandConfigModules function)
- Go: `go-engine/internal/modules/synthesize.go` (SynthesizeAppsFromModules function)
- Go: `go-engine/internal/manifest/types.go` (Manifest struct ExcludeConfigs field)

## See Also
- `profile-composition` spec (defines excludeConfigs as part of the profile composition model)
- `restore-opt-in` spec (restore requires explicit opt-in)
- `separation-of-concerns` spec (install, configure, verify are distinct stages)
