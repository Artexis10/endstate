## ADDED Requirements

### Requirement: Read-only extracted-profile inspection
The system SHALL provide `endstate profile inspect <manifest-path> --json` as a read-only command. It MUST accept only an extracted manifest path and may read the extracted artifact files, inspect-only includes, sibling capture/config metadata, verified `provenance/modules/` snapshots, and the trusted current module catalog through its pure loader. The catalog is enrichment-only: no catalog entry may create ownership and no matcher may run. The command MUST NOT extract a bundle; invoke drivers or matchers; detect current-machine state; plan, preview, apply, restore, or mutate data.

Inspect-only includes MUST be relative `.json`, `.jsonc`, or `.json5` paths that resolve inside the root manifest directory. Absolute, extensionless/profile-name, directory, bundle, and root-escaping includes MUST fail with `MANIFEST_VALIDATION_ERROR`; inspection MUST NOT extract them. The command MUST observe composition and apply only the root manifest's exact `refs.windows` `exclude` to Apps and root-only `excludeConfigs` to settings ownership; included manifests' exclusions MUST be ignored.

#### Scenario: Inspect an extracted composed manifest
- **WHEN** a valid extracted root manifest contains allowed relative includes and root exclusions
- **THEN** the engine returns the composed inventory after applying only the root exclusions without host evaluation or mutation

#### Scenario: Reject an unsupported include
- **WHEN** inspection encounters an absolute, extensionless, directory, bundle, or root-escaping include
- **THEN** it returns `MANIFEST_VALIDATION_ERROR` without profile-name resolution or bundle extraction

### Requirement: Canonical saved-profile ownership
The engine SHALL treat saved-profile evidence as authoritative for settings ownership. It MUST canonicalize module IDs by trimming whitespace, lowercasing for comparison, and stripping exactly one leading `apps.`; it MUST NOT fuzzy-match IDs and MUST preserve contributing raw module IDs as technical detail. Ownership MUST be the union of all applicable evidence, deduplicated by canonical key. A stronger source records provenance but MUST NOT suppress a module found only in a lower tier.

For v2, applicable sources are `configCaptures[].moduleId` and `legacyConfigLanes[].moduleId`. For v1, applicable sources in descending authority are `restore[].fromModule`, `configModules[]`, sibling `metadata.json.configModulesIncluded[]`, and the first segment after `configs/` in each restore `source`. Catalog membership alone MUST NOT establish ownership.

#### Scenario: Retain lower-tier-only ownership
- **WHEN** a module occurs only in sibling metadata or a `configs/<module>/...` source
- **THEN** the engine returns that canonical module as owned even if no stronger source names it

#### Scenario: Deduplicate canonical module aliases
- **WHEN** `apps.Obsidian` and ` obsidian ` occur in applicable evidence
- **THEN** the engine emits one owned canonical module row while retaining the raw contributing IDs as details

### Requirement: Deterministic inventory grouping and association
The inspection success envelope SHALL use schema 1.x, retain top-level `command: "profile"`, and contain non-null deterministic `apps`, `settingsApps`, and `warnings` arrays. Every owned module MUST be represented exactly once before grouping. The engine MUST group every `included` module that shares one unique Apps row and every `not_in_profile` module that shares one verified absent-owner identity. It MUST NOT group `ambiguous` or `unresolved` modules.

For every owned module, association MUST select exactly one verified owner-ref tier in this order: non-empty `configCaptures[].sourceInstance.evidence.ref` values; package refs from successfully verified embedded snapshots; then package refs from the trusted current catalog for that already-owned module. The first non-empty tier MUST be the selected owner-ref set. Lower tiers MAY supply labels but MUST NOT add association candidates or override a higher tier. Within that set, refs MUST be trimmed, case-insensitively deduplicated, and sorted. Failure to verify an intended higher tier MUST emit the contracted warning and permit the next tier.

An Apps-row candidate MUST be an Apps row whose normalized `packageRefs[]` intersects the selected owner-ref set by exact case-insensitive equality. Exactly one candidate MUST yield `included`; more than one MUST yield `ambiguous`; zero candidates with a non-empty selected ref set MUST yield `not_in_profile`; and an empty selected ref set MUST yield `unresolved`.

Every Apps inventory entry MUST have `id`, `manifestAppId`, `displayName`, non-null `packageRefs`, and `hasSettings`. Its `id` MUST be assigned before presentation sorting as `app:<case-folded-manifest-app-id-or-unnamed>:<one-based-occurrence-among-that-case-folded-id-in-resolved-manifest-order>`. `manifestAppId` preserves the raw manifest app identifier. Each settings row MUST have exactly one association status: `included`, `not_in_profile`, `ambiguous`, or `unresolved`. `ownerId` MUST be non-null only for `included` and `not_in_profile`; `appId` MUST be non-null only for `included`; `appIncluded` MUST be true iff the status is `included`; and `candidateAppIds` MUST contain the single Apps row `id` for `included`, sorted Apps row IDs for `ambiguous`, and `[]` otherwise. Settings row IDs MUST be `settings:<app-row-id>` for included, `settings:<absent-owner-key>` for not-in-profile, and `settings:module:<canonical-module-key>` for ambiguous or unresolved. `ownerId` for included is the Apps row `id`.

For `not_in_profile`, the absent-owner key MUST be `package:` plus the complete sorted case-folded selected-ref set joined by `|`; `ownerId` MUST use that key, and modules MAY group only when their entire key is identical. Only `included` can mark the referenced Apps row `hasSettings` true.

`included` and `not_in_profile` rows SHALL contribute to `verifiedSettingsAppCount`; `ambiguous` and `unresolved` rows SHALL contribute to `unidentifiedSettingsRowCount`. A short owned module ID MAY associate through verified trusted-catalog package refs with a manifest Apps row having a different ID.

#### Scenario: Group verified included modules
- **WHEN** two owned modules uniquely associate to the same included Apps row
- **THEN** the engine returns one `included` row with both module IDs and row ID `settings:<app-row-id>`

#### Scenario: Preserve duplicate-App ambiguity separately
- **WHEN** two resolved Apps rows have the same verified owner ref, including duplicate manifest app identifiers preserved by composition
- **THEN** the engine returns an `ambiguous` `settings:module:<canonical-module-key>` row with sorted Apps row IDs, no owner/app ID, and no marked Apps row

### Requirement: Deterministic fields, labels, counts, and ordering
`data.profile` MUST contain nullable `name`, nullable `capturedAt`, integer `manifestVersion`, and string `manifestPath`. Non-empty manifest `captured` MUST win for `capturedAt`; otherwise non-empty sibling metadata `capturedAt` MUST be used; otherwise it is null. A conflict MAY produce a diagnostic warning without changing that order.

Apps display names MUST use captured app `displayName`, then first sorted package ref, then humanized manifest app ID. App package refs MUST include every non-empty `refs` value, trimmed, deduplicated, and sorted. Each `settingsApps[]` row MUST contain `id`, `displayName`, `associationStatus`, nullable `ownerId`, nullable `appId`, `appIncluded`, non-null `packageRefs`, non-null `moduleIds`, non-null `candidateAppIds`, and non-negative `capturedEntryCount`. `settingsApps[].packageRefs` MUST be the sorted union of selected verified owner refs for its contributing modules. Settings display names MUST use verified snapshot `displayName`, trusted-catalog `displayName`, associated app `displayName`, first sorted verified package ref, then a humanized canonical module key. Raw provenance IDs MUST NOT be default labels.

For v2, `capturedEntryCount` MUST sum `payloadManifest.length` once per distinct `captureId` and count restore entries bound to each legacy lane's `legacyCaptureId`. For v1, it MUST count distinct restore-array entries attributed by `fromModule`, falling back per entry to `configs/<module>/...` source attribution. Metadata-only/configModules-only ownership MUST count zero. A grouped row MUST sum its contributing module counts without double-counting an entry.

Apps and settings rows MUST sort by case-folded display name then stable row ID. `packageRefs`, `moduleIds`, and `candidateAppIds` MUST sort by case-folded value then original value. Warnings MUST sort by code then message. `data.summary` MUST contain non-negative `appCount`, `settingsRowCount`, `verifiedSettingsAppCount`, and `unidentifiedSettingsRowCount`, derived from the finalized grouped arrays. Each warning MUST contain `code`, engine-authored `message`, and `impact`, where impact is exactly `diagnostic` or `inventory_incomplete`.

#### Scenario: Count a grouped v2 row without duplication
- **WHEN** module A has distinct capture IDs with payload-manifest lengths 2 and 3, module B has a third distinct capture ID with length 1, and both verify to the same Apps row
- **THEN** one grouped row has `capturedEntryCount` 6, because manifest validation guarantees unique capture IDs and counting defensively deduplicates

#### Scenario: Return metadata-only v1 ownership
- **WHEN** a v1 module is owned only through `configModules[]` or sibling metadata
- **THEN** its settings row is retained with `capturedEntryCount` zero

### Requirement: Capability-gated profile inspection
The capabilities response SHALL expose `features.profileInspection: true` when this command is supported. Consumers MUST gate inspection on this boolean. A consumer without it MUST show update-required and MUST NOT infer ownership from manifests, catalogs, snapshots, or current-machine state.

#### Scenario: Use a stale engine
- **WHEN** capabilities omits or sets `features.profileInspection` to false
- **THEN** the GUI shows update-required and makes no fallback ownership claim

### Requirement: Structured manifest and usage failures
The command SHALL return standard structured error envelopes. Missing paths MUST be a structured usage failure and MUST NOT panic or write a human-only stdout result. Missing, unparsable, and invalid manifest inputs MUST use `MANIFEST_NOT_FOUND`, `MANIFEST_PARSE_ERROR`, and `MANIFEST_VALIDATION_ERROR`, respectively, where applicable.

#### Scenario: Missing manifest path
- **WHEN** `profile inspect --json` is invoked without a manifest path
- **THEN** the command returns a structured usage failure without a panic
