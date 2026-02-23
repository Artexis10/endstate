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
- **AND** the envelope data contains `restoreModulesAvailable` listing all modules that had restore entries

#### Scenario: restoreModulesAvailable shows all available modules

- **WHEN** `apply --EnableRestore --RestoreFilter apps.vscode --json` is run with modules apps.vscode and apps.git both having restore entries
- **THEN** `restoreModulesAvailable` contains both "apps.vscode" and "apps.git"
- **AND** only apps.vscode restore entries were executed

#### Scenario: RestoreFilter on standalone restore command

- **WHEN** `restore --EnableRestore --RestoreFilter apps.vscode` is run
- **THEN** only restore entries from module apps.vscode are executed
- **AND** entries from other modules are skipped

#### Scenario: Capabilities include restore-filter flag

- **WHEN** `capabilities --json` is run
- **THEN** commands.apply.flags includes "--restore-filter"
- **AND** commands.restore.flags includes "--restore-filter"
