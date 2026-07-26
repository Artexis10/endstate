## ADDED Requirements

### Requirement: Extensionless Includes Resolve as Profile Names
Entries in a manifest's `includes` array SHALL be discriminated by file extension. An entry WITH a recognised extension (`.jsonc`, `.json`, `.yaml`, `.yml`, `.zip`) SHALL be resolved as a file path relative to the manifest directory (existing behaviour). An entry WITHOUT an extension SHALL be resolved as a profile name via `Resolve-ProfilePath` against the canonical profiles directory (`Documents\Endstate\Profiles\`). There SHALL be no fallback between the two forms.

#### Scenario: Extensionless entry resolves as a profile name
- **WHEN** a manifest has `includes` containing `"my-desktop"` and `my-desktop.zip` exists in the profiles directory
- **THEN** the include SHALL resolve to that bundle and its `manifest.jsonc` SHALL be merged into the parent

#### Scenario: Entry with an extension resolves as a file path
- **WHEN** a manifest has `includes` containing `"./laptop-dev-extras.jsonc"`
- **THEN** the include SHALL be resolved as a file path relative to the manifest directory
- **AND** the profiles directory SHALL NOT be searched for it

#### Scenario: Profile name resolution follows zip, folder, bare precedence
- **WHEN** an extensionless include is resolved
- **THEN** `Resolve-ProfilePath` SHALL be used, returning zip, folder, or bare format in that precedence order
- **AND** only `Documents\Endstate\Profiles\` SHALL be searched

#### Scenario: Unresolvable profile name is an error
- **WHEN** an extensionless include names a profile that does not exist in the profiles directory
- **THEN** resolution SHALL fail with the error `Included profile not found: {name}`

#### Scenario: Transitive includes are resolved recursively
- **WHEN** an included profile declares its own `includes`
- **THEN** those includes SHALL be resolved recursively (existing merge behaviour)

### Requirement: Exclude Field Removes Apps by Winget ID
Manifests SHALL support an optional `exclude` array of winget IDs. After all includes are merged, any app whose `refs.windows` matches an `exclude` entry by exact string SHALL be removed from the merged app list. Matching SHALL NOT use wildcards or partial matching, and SHALL match `refs.windows` rather than the app `id`.

#### Scenario: Excluded app is removed from the merged list
- **WHEN** a root profile includes a base with an app whose `refs.windows` is `Seagate.SeaTools`
- **AND** the root profile's `exclude` contains `Seagate.SeaTools`
- **THEN** that app SHALL NOT appear in the resolved app list

#### Scenario: Exclude matches winget ID, not app id
- **WHEN** an `exclude` entry matches an app's `id` but not its `refs.windows`
- **THEN** the app SHALL remain in the resolved app list

#### Scenario: Exclude requires an exact match
- **WHEN** an `exclude` entry is a prefix or substring of an app's `refs.windows`
- **THEN** the app SHALL NOT be removed

#### Scenario: Non-matching exclude entry is inert
- **WHEN** an `exclude` entry matches no app in the merged list
- **THEN** resolution SHALL succeed and the app list SHALL be unchanged

### Requirement: ExcludeConfigs Field Suppresses Config Restore
Manifests SHALL support an optional `excludeConfigs` array of config module IDs. After all includes are merged, config restore for each named module SHALL be suppressed. The corresponding app SHALL still be installed — only config restoration is skipped.

#### Scenario: Named config module is suppressed
- **WHEN** a root profile's `excludeConfigs` contains `powertoys`
- **THEN** the `powertoys` config module SHALL NOT be restored
- **AND** the PowerToys app SHALL still be installed

#### Scenario: Config modules not listed are unaffected
- **WHEN** `excludeConfigs` contains `powertoys` only
- **THEN** all other config modules SHALL be restored as normal

### Requirement: Exclude Implies Config Suppression
An app removed via `exclude` SHALL also have its associated config module payloads suppressed. Listing an excluded app in both `exclude` and `excludeConfigs` SHALL NOT be required.

#### Scenario: Excluded app's config is suppressed automatically
- **WHEN** an app is listed in `exclude` and its config module is NOT listed in `excludeConfigs`
- **THEN** neither the app nor its config module SHALL appear in the resolved plan

### Requirement: Exclusions Are Not Inherited from Included Profiles
Composition SHALL be single-depth for exclusions. Only the root profile's `exclude` and `excludeConfigs` SHALL be applied. An included profile's own `exclude` and `excludeConfigs` SHALL be ignored, so the final plan is always determinable from the root manifest alone.

#### Scenario: Included profile's exclusions are ignored
- **WHEN** an included profile declares `exclude` containing `Some.App`
- **AND** the root profile does not list `Some.App` in its own `exclude`
- **THEN** `Some.App` SHALL remain in the resolved app list

#### Scenario: Root exclusions still apply across all merged sources
- **WHEN** the root profile's `exclude` names an app contributed by a transitively included profile
- **THEN** that app SHALL be removed from the resolved app list

### Requirement: Included Zip Temp Directories Survive Until Run Completion
When an included profile resolves to a zip bundle, it SHALL be extracted via `Expand-ProfileBundle` and the extracted temp directory SHALL remain accessible for the duration of the apply/verify run, so config payloads from the extracted `configs/` directory are available to `--enable-restore`. Cleanup of all tracked temp directories SHALL occur after run completion, on both success and failure.

#### Scenario: Extracted configs are available during restore
- **WHEN** an included profile resolves to a zip and apply runs with `--enable-restore`
- **THEN** config payloads from the extracted `configs/` directory SHALL be readable throughout the run

#### Scenario: Temp directories are cleaned up after a successful run
- **WHEN** an apply run that extracted included bundles completes successfully
- **THEN** all tracked temp directories SHALL be removed in the `finally` block

#### Scenario: Temp directories are cleaned up after a failed run
- **WHEN** an apply run that extracted included bundles fails
- **THEN** all tracked temp directories SHALL still be removed in the `finally` block

### Requirement: Composition Fields Are Optional and Backward Compatible
`exclude` and `excludeConfigs` SHALL be additive optional fields defaulted to empty arrays by `Normalize-Manifest`. Existing manifests that omit them, or that use file-path-only includes, SHALL continue to resolve exactly as before. No schema version bump SHALL be required.

#### Scenario: Manifest without composition fields is unaffected
- **WHEN** a manifest declares neither `exclude` nor `excludeConfigs`
- **THEN** `Normalize-Manifest` SHALL default both to empty arrays
- **AND** resolution SHALL behave identically to before this capability was introduced

#### Scenario: File-path-only includes keep working
- **WHEN** an existing manifest uses only file-path includes
- **THEN** include resolution SHALL be unchanged

#### Scenario: App list merge stays additive
- **WHEN** the same app ID appears in both an included profile and the root profile
- **THEN** both entries SHALL be preserved in the merged list (no dedup)
- **AND** idempotent apply SHALL skip the duplicate as already installed
