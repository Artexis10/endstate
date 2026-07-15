## ADDED Requirements

### Requirement: Engine Resolves Every Captured Config Set Before Mutation
Before writing target configuration, the engine SHALL produce a per-capture resolution of `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified` with a stable machine-readable reason and the source/target generations when known.

#### Scenario: Same generation resolves direct
- **WHEN** source and target config generations are identical
- **THEN** resolution is `direct`
- **AND** no migration operations are planned

#### Scenario: Unique forward path resolves migrate
- **WHEN** source and target generations differ and the current catalog has one valid forward migration path
- **THEN** resolution is `migrate`
- **AND** the ordered generation path is included in the plan

#### Scenario: Older target rejects downgrade
- **WHEN** the target generation order is lower than the source generation order
- **THEN** resolution is `incompatible`
- **AND** reason is `downgrade_unsupported`

#### Scenario: Missing knowledge remains unknown
- **WHEN** target version or generation cannot be determined safely
- **THEN** resolution is `unknown`
- **AND** no config mutation occurs for that set

### Requirement: Target Instance Selection Never Guesses Latest
The engine SHALL automatically select a target instance only when there is one viable target or one unique exact-version target. Multiple viable targets SHALL produce `ambiguous_target_instance` until the caller provides an explicit valid mapping.

#### Scenario: One target is selected automatically
- **WHEN** exactly one compatible target instance exists
- **THEN** the engine selects it without requiring an explicit mapping

#### Scenario: Two compatible targets are ambiguous
- **WHEN** two side-by-side target instances can accept a captured config set
- **THEN** resolution is `unknown`
- **AND** reason is `ambiguous_target_instance`
- **AND** the engine does not select the highest version

#### Scenario: Unique exact version wins over non-exact candidate
- **WHEN** exactly one target instance matches the captured raw/normalized app version and another target is only generation-compatible
- **THEN** the unique exact-version instance is selected

### Requirement: Explicit Target Mappings Are Validated During Preflight
An explicit capture-to-target mapping SHALL reference an existing capture ID and detected target instance, and the selected target SHALL still pass generation compatibility. Malformed mappings, duplicate mappings for one capture ID, and mappings to unknown capture IDs SHALL be command-input errors before installation or configuration mutation. A syntactically valid target ID that is absent or incompatible after final post-install detection SHALL skip only the affected config set with a stable reason.

#### Scenario: Valid explicit mapping resolves ambiguity
- **WHEN** the caller maps an ambiguous capture ID to one compatible detected target instance
- **THEN** the engine resolves against that target

#### Scenario: Mapping to incompatible target is rejected
- **WHEN** an explicit mapping selects a target with no supported direct or forward path
- **THEN** the affected config set is skipped with reason `mapped_target_incompatible`
- **AND** no config mutation occurs
- **AND** successful application installation remains intact

#### Scenario: Mapping input is malformed or duplicated
- **WHEN** a restore-target mapping is malformed, duplicates a capture ID, or names an unknown capture ID
- **THEN** the command returns an input error before installation or config mutation

#### Scenario: Mapped target is absent after install
- **WHEN** a syntactically valid mapped target instance is not detected after rebuild installation
- **THEN** the affected config set is skipped with reason `mapped_target_not_detected`
- **AND** application installation is not rolled back

### Requirement: Restore Planning Detects Target Collisions
The engine SHALL reject selected config sets whose resolved target paths are equal, whose target directories overlap by parent/child containment, or whose captured sources compete for the same target instance/config set.

#### Scenario: Exact target collision is blocked
- **WHEN** two selected config sets resolve to the same target file
- **THEN** both are blocked with reason `target_collision`
- **AND** neither target action executes

#### Scenario: Parent-child target overlap is blocked
- **WHEN** one selected action targets a directory containing another selected action's target
- **THEN** the overlapping sets are blocked before mutation

### Requirement: Rebuild Re-Detects Targets After Installation
Rebuild SHALL perform final target instance and generation detection after application installation and immediately before configuration restore. The catalog revision used for the run SHALL remain pinned throughout planning and execution.

#### Scenario: Previously absent app becomes detectable
- **WHEN** rebuild installs an application that was absent during initial planning
- **THEN** the engine detects its actual installed instance/version after installation
- **AND** produces the final config resolution before configuration mutation

#### Scenario: Unpinned install differs from preview evidence
- **WHEN** an unpinned installation yields a different version than pre-install evidence suggested
- **THEN** final resolution uses the installed version
- **AND** no stale pre-install generation decision is executed

### Requirement: Config Compatibility Does Not Block Independent Installation or Sets
An incompatible, unknown, ambiguous, or failed config set SHALL be skipped before its mutation without undoing successful application installation or blocking unrelated config sets whose plans are safe.

#### Scenario: App installs while settings are incompatible
- **WHEN** application installation succeeds but its captured settings resolve `incompatible`
- **THEN** the app remains installed
- **AND** the incompatible config set is skipped with a structured result

#### Scenario: One set failure does not block another set
- **WHEN** one app's `workspaces` set is incompatible and its `preferences` set is direct
- **THEN** `preferences` may restore
- **AND** `workspaces` remains unchanged

### Requirement: Resolution Uses One Trusted Catalog Snapshot Per Run
The engine SHALL load and pin one trusted catalog snapshot and its module revisions for the duration of a run. Bundle-supplied module snapshots SHALL NOT replace or mutate that catalog.

#### Scenario: Catalog files change during a run
- **WHEN** on-disk catalog content changes after planning begins
- **THEN** the running plan continues with its pinned in-memory catalog snapshot
- **AND** reports the pinned restore-time module revision

### Requirement: Missing or Changed Current Catalog Knowledge Never Falls Back
If the pinned current catalog lacks the captured module ID, config-set ID, or source-generation identity, or if the same generation ID has a different unaccepted fingerprint, the engine SHALL resolve the set as `unknown` without config mutation or legacy fallback.

#### Scenario: Current module is missing
- **WHEN** the current catalog has no module matching a v2 capture's module ID
- **THEN** resolution is `unknown` with reason `catalog_module_missing`
- **AND** no config mutation or legacy fallback occurs

#### Scenario: Current config set is missing
- **WHEN** the current module lacks the captured config-set ID
- **THEN** resolution is `unknown` with reason `config_set_missing`
- **AND** no config mutation occurs

#### Scenario: Source generation identity is missing
- **WHEN** the current config set neither defines the captured source generation nor explicitly accepts its fingerprint
- **THEN** resolution is `unknown` with reason `source_generation_unknown`
- **AND** no config mutation occurs

#### Scenario: Source generation meaning changed
- **WHEN** the current generation ID matches but its fingerprint differs and the captured fingerprint is not explicitly accepted
- **THEN** resolution is `unknown` with reason `source_generation_definition_changed`
- **AND** no config mutation occurs
