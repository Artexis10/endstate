## ADDED Requirements

### Requirement: Read-only extracted-profile inspection
The system SHALL provide `endstate profile inspect <manifest-path> --json` as a read-only inspection command. It MUST accept only an extracted manifest path and MUST read only the manifest, recursively resolved includes, sibling capture metadata, declared config payload metadata, and verified `provenance/modules/` snapshots. It MUST NOT extract a bundle; invoke drivers or matchers; detect current-machine state; plan, preview, apply, restore, or mutate any data.

#### Scenario: Inspect an extracted manifest
- **WHEN** a valid extracted manifest path is passed to `profile inspect --json`
- **THEN** the engine returns its saved-profile inventory without host evaluation or mutation

#### Scenario: Reject a bundle or directory input
- **WHEN** a bundle path or directory path is passed to `profile inspect --json`
- **THEN** the engine returns a structured usage failure and does not extract or inspect it

### Requirement: Deterministic saved-profile inventory
The inspection success envelope SHALL use schema 1.x, retain top-level `command: "profile"`, and contain non-null deterministically ordered `apps`, `settingsApps`, and `warnings` arrays. `data.profile` MUST contain nullable `name`, nullable `capturedAt`, integer `manifestVersion`, and string `manifestPath`. `data.summary` MUST contain non-negative `appCount`, `settingsRowCount`, `verifiedSettingsAppCount`, and `unidentifiedSettingsRowCount` derived from finalized arrays.

#### Scenario: Return an install-only profile
- **WHEN** an inspected profile owns no settings modules
- **THEN** `settingsApps` and `warnings` are `[]` and every summary count is derived from the returned arrays

#### Scenario: Return a profile with settings
- **WHEN** an inspected profile contains owned settings modules
- **THEN** every profile-owned module is represented exactly once before verified-owner grouping and every array has stable deterministic ordering

### Requirement: Saved-profile ownership and association
The engine SHALL treat saved-profile evidence as authoritative for settings ownership. For v2, ownership MUST be the deduplicated union of distinct `configCaptures[].moduleId` values and explicitly declared legacy config lanes. For v1, ownership evidence MUST be considered in descending authority: explicit `restore[].fromModule`, declared captured config-module metadata, sibling capture metadata, and old `configs/<module-id>/...` restore sources. Catalog membership alone MUST NOT establish ownership.

The engine SHALL assign each settings-app row exactly one association status: `included`, `not_in_profile`, `ambiguous`, or `unresolved`. Only `included` MAY set an Apps row's `hasSettings` to true. `included` and `not_in_profile` rows SHALL count as verified app-settings owners; `ambiguous` and `unresolved` rows SHALL count as unidentified settings rows and MUST NOT mark an app. Modules MAY be grouped only when they share one verified owner; otherwise they remain separate. A short owned module ID MAY associate through verified catalog package references to an Apps row with a distinct app ID.

#### Scenario: Associate a legacy short module through its package ref
- **WHEN** owned module `obsidian` has verified catalog ref `Obsidian.Obsidian` and the manifest app is `obsidian-obsidian` with that ref
- **THEN** the settings row is associated with that app without requiring exact app-ID equality

#### Scenario: Preserve ambiguous ownership
- **WHEN** a profile-owned module has multiple possible manifest app owners
- **THEN** the engine returns an `ambiguous` settings row with no marked Apps row and includes it in the unidentified count

### Requirement: Engine-authored presentation semantics
Each `apps[]` row SHALL contain `id`, `displayName`, non-null `packageRefs`, and `hasSettings`. Each `settingsApps[]` row SHALL contain deterministic `id`, `displayName`, `associationStatus`, nullable `ownerId`, nullable `appId`, `appIncluded`, non-null `packageRefs`, non-null `moduleIds`, non-null `candidateAppIds`, and non-negative `capturedEntryCount`. Friendly labels MUST prefer a verified embedded snapshot, captured module metadata, a catalog display name for an already-owned module, an associated manifest app display name/package ref, and a deterministic human-readable short module ID, in that order. Raw provenance IDs MUST NOT be default labels.

Each warning SHALL contain `code`, engine-authored `message`, and `impact`, where `impact` is exactly `diagnostic` or `inventory_incomplete`.

#### Scenario: Use verified snapshot labels
- **WHEN** a verified embedded module snapshot supplies a display name
- **THEN** the engine uses that name ahead of catalog and manifest-app fallback labels

#### Scenario: Render an incomplete legacy inventory
- **WHEN** ownership evidence cannot resolve a captured settings module
- **THEN** the engine retains a separate `unresolved` row and returns an `inventory_incomplete` warning instead of inventing an owner

### Requirement: Capability-gated profile inspection
The capabilities response SHALL expose `features.profileInspection: true` when this command is supported. Consumers MUST gate inspection on this boolean, and a consumer without it MUST show an update-required state rather than infer ownership from manifests, catalogs, snapshots, or current-machine state.

#### Scenario: Use a compatible engine
- **WHEN** capabilities returns `features.profileInspection: true`
- **THEN** the GUI invokes `profile inspect` and renders the engine-provided semantics

#### Scenario: Use a stale engine
- **WHEN** capabilities omits or sets `features.profileInspection` to false
- **THEN** the GUI shows update-required and makes no fallback ownership claim

### Requirement: Structured manifest and usage failures
The command SHALL return standard structured error envelopes. Missing paths MUST be a structured usage failure and MUST NOT panic or write a human-only stdout result. Missing, unparsable, and invalid manifest inputs MUST use `MANIFEST_NOT_FOUND`, `MANIFEST_PARSE_ERROR`, and `MANIFEST_VALIDATION_ERROR`, respectively, where applicable.

#### Scenario: Missing manifest path
- **WHEN** `profile inspect --json` is invoked without a manifest path
- **THEN** the command returns a structured usage failure without a panic

#### Scenario: Invalid manifest
- **WHEN** the provided extracted manifest fails parsing or validation
- **THEN** the command returns the corresponding standard manifest error envelope
