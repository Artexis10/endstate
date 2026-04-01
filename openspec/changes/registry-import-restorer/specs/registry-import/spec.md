## ADDED Requirements

### Requirement: registry-import restore type

The engine SHALL support a `registry-import` restore strategy that imports a `.reg` file into the Windows Registry.

#### Scenario: Restore imports .reg file

- **GIVEN** a restore action with `type: "registry-import"` and `source` pointing to a valid `.reg` file
- **WHEN** the restore is executed
- **THEN** the engine runs `reg import <resolved_source_path>`
- **AND** the result status is `"restored"`

#### Scenario: Backup before import

- **GIVEN** a restore action with `type: "registry-import"`, `backup: true`, and the target registry key exists
- **WHEN** the restore is executed
- **THEN** the engine runs `reg export <target_key> <backup.reg> /y` before importing
- **AND** the journal entry records the backup path in `backupPath`
- **AND** `backupCreated` is `true`

#### Scenario: HKCU keys only

- **GIVEN** a restore action with `type: "registry-import"` and `target` starting with `HKLM\` or `HKEY_LOCAL_MACHINE\`
- **WHEN** the restore is executed
- **THEN** the operation fails with error `"registry-import only supports HKCU keys"`

#### Scenario: Non-Windows platform

- **GIVEN** the engine is running on macOS or Linux
- **WHEN** a `registry-import` restore action is executed
- **THEN** the operation fails with error `"registry-import is only supported on Windows"`

#### Scenario: Optional entry with missing source

- **GIVEN** a restore action with `type: "registry-import"`, `optional: true`, and the source `.reg` file does not exist
- **WHEN** the restore is executed
- **THEN** the action is skipped with status `"skipped_missing_source"`

### Requirement: Registry capture support

The module capture system SHALL support a `registryKeys` field for exporting registry keys to `.reg` files.

#### Scenario: Capture exports registry key

- **GIVEN** a module with a `registryKeys` entry specifying a `key` and `dest`
- **AND** the registry key exists
- **WHEN** capture is executed
- **THEN** the engine runs `reg export <key> <dest.reg> /y`
- **AND** the exported `.reg` file is included in the capture payload

#### Scenario: Optional registry key missing

- **GIVEN** a module with a `registryKeys` entry where `optional: true`
- **AND** the specified registry key does not exist
- **WHEN** capture is executed
- **THEN** the entry is silently skipped

### Requirement: Revert supports registry-import

The revert system SHALL import the backup `.reg` file for `registry-import` journal entries, rather than copying it as a filesystem file.

#### Scenario: Revert restores backup registry state

- **GIVEN** a journal entry for a `registry-import` restore with `backupCreated: true` and a valid backup `.reg` file
- **WHEN** revert is executed
- **THEN** the engine runs `reg import <backup.reg>`
- **AND** the revert result action is `"reverted"`
