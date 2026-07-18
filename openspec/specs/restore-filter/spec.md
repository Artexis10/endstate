# restore-filter Specification

## Purpose
Defines the --RestoreFilter flag for apply and restore commands, enabling per-module config selection during restore operations.
## Requirements
### Requirement: RestoreFilter for Per-Module Config Selection

The apply and restore commands SHALL support a --RestoreFilter flag that limits restore execution to specified config modules.

#### Scenario: Filter restricts restore to listed modules

- **WHEN** `apply --EnableRestore --RestoreFilter apps.vscode,apps.git` is run
- **THEN** only restore entries from modules apps.vscode and apps.git are executed
- **AND** restore entries from other modules are skipped

#### Scenario: No filter restores all modules

- **WHEN** `apply --EnableRestore` is run WITHOUT --RestoreFilter
- **THEN** all restore entries from all modules are executed (backward compatible)

#### Scenario: Inline restore entries bypass filter

- **WHEN** `apply --EnableRestore --RestoreFilter apps.vscode` is run with manifest containing both configModule entries and inline restore entries
- **THEN** inline restore entries (those without _fromModule) are always executed regardless of filter

#### Scenario: restoreFilter in JSON envelope

- **WHEN** `apply --EnableRestore --RestoreFilter apps.vscode --json` is run
- **THEN** the JSON envelope data contains `restoreFilter: ["apps.vscode"]`
- **AND** the envelope data contains `restoreModulesAvailable` listing all modules that had restore entries as enriched objects with `id` and `displayName` fields

#### Scenario: restoreModulesAvailable shows all available modules

- **WHEN** `apply --EnableRestore --RestoreFilter apps.vscode --json` is run with modules apps.vscode and apps.git both having restore entries
- **THEN** `restoreModulesAvailable` contains objects for both "apps.vscode" and "apps.git" with their respective `id` and `displayName` fields
- **AND** only apps.vscode restore entries were executed

#### Scenario: RestoreFilter on standalone restore command

- **WHEN** `restore --EnableRestore --RestoreFilter apps.vscode` is run
- **THEN** only restore entries from module apps.vscode are executed
- **AND** entries from other modules are skipped

#### Scenario: Capabilities include restore-filter flag

- **WHEN** `capabilities --json` is run
- **THEN** commands.apply.flags includes "--restore-filter"
- **AND** commands.restore.flags includes "--restore-filter"

### Requirement: Restore Target Selects a Specific Detected Instance
Apply, standalone restore, and rebuild SHALL support repeatable `--restore-target <captureId>=<targetInstanceId>` arguments for resolving ambiguous generation-aware captures. Module-level `--restore-filter` behavior SHALL remain unchanged and SHALL apply before per-capture target mapping.

#### Scenario: Explicit target selects one side-by-side instance
- **WHEN** two compatible target instances exist and the caller supplies a valid `--restore-target` mapping
- **THEN** only the mapped instance receives that captured config set

#### Scenario: No explicit target for unambiguous set
- **WHEN** exactly one viable target instance exists
- **THEN** restore may proceed without `--restore-target`

#### Scenario: Invalid target mapping fails preflight
- **WHEN** a mapping is malformed, duplicates a capture mapping, or references an unknown capture ID
- **THEN** the command returns `INVALID_RESTORE_TARGET` before installation or config mutation
- **AND** the error includes an engine-authored message and remediation

#### Scenario: Mapped target cannot be used after install
- **WHEN** a syntactically valid target ID is absent or incompatible after final post-install detection
- **THEN** only the affected config set is skipped with `mapped_target_not_detected` or `mapped_target_incompatible`
- **AND** successful application installation remains intact

#### Scenario: Module filter excludes mapped capture
- **WHEN** `--restore-filter` excludes the module owning a supplied target mapping
- **THEN** that capture remains excluded
- **AND** the mapping does not bypass the module filter

#### Scenario: Capabilities advertise restore-target
- **WHEN** `capabilities --json` is run
- **THEN** the apply, restore, and rebuild command capabilities include `--restore-target`
