## ADDED Requirements

### Requirement: registry-set restore type

The engine SHALL support a `registry-set` restore strategy that sets a single named value under an HKCU registry key, touching no other value under that key.

#### Scenario: Set a named value

- **GIVEN** a restore action with `type: "registry-set"`, an HKCU `key`, a `valueName`, a supported `valueType`, and `data`
- **WHEN** the restore is executed and the value is not already equal to the desired type+data
- **THEN** the engine creates the key if missing and writes the named value
- **AND** the result status is `"restored"`

#### Scenario: Backup the prior value before overwrite

- **GIVEN** a `registry-set` action whose target value will change
- **WHEN** the restore is executed
- **THEN** the engine records the prior value (its REG_* type and data, or that it was absent) to `state/backups/<runID>/` BEFORE writing
- **AND** the journal entry records the backup path in `backupPath`
- **AND** `backupCreated` is `true`

#### Scenario: Idempotent skip when already equal

- **GIVEN** a `registry-set` action whose named value already equals the desired type+data
- **WHEN** the restore is executed
- **THEN** the action is skipped with status `"skipped_up_to_date"`
- **AND** no backup is written

#### Scenario: DWORD data compares numerically

- **GIVEN** a `registry-set` action with `valueType: "REG_DWORD"` and `data: "0x1"`
- **AND** the stored value is the DWORD `1`
- **WHEN** the restore is executed
- **THEN** the action is treated as already-equal and skipped

#### Scenario: HKCU keys only

- **GIVEN** a `registry-set` action whose `key` starts with `HKLM\`, `HKCR\`, `HKU\`, or `HKCC\`
- **WHEN** the restore is executed
- **THEN** the operation fails with an error indicating only HKCU keys are supported

#### Scenario: Unsupported value type rejected

- **GIVEN** a `registry-set` action with a `valueType` other than REG_DWORD, REG_SZ, or REG_EXPAND_SZ
- **WHEN** the restore is executed
- **THEN** the operation fails with an error naming the supported value types

#### Scenario: Dry-run makes no change

- **GIVEN** a `registry-set` action and dry-run enabled
- **WHEN** the restore is executed
- **THEN** the result status is `"restored"`
- **AND** neither the registry value nor a backup sidecar is written

#### Scenario: Non-Windows platform

- **GIVEN** the engine is running on macOS or Linux
- **WHEN** a `registry-set` restore action is executed
- **THEN** the operation fails with an error indicating registry-set is only supported on Windows

### Requirement: registry-set revert

The revert system SHALL undo a `registry-set` write by restoring the exact prior value, or deleting the value when it was absent before the write.

#### Scenario: Revert restores prior data

- **GIVEN** a journal entry for a `registry-set` restore whose backup records a prior value
- **WHEN** revert is executed
- **THEN** the engine restores the prior REG_* type and data
- **AND** the revert result action is `"reverted"`

#### Scenario: Revert deletes a created value

- **GIVEN** a journal entry for a `registry-set` restore whose backup records that the value was absent before
- **WHEN** revert is executed
- **THEN** the engine deletes the named value
- **AND** the revert result action is `"deleted"`

### Requirement: registry-value-equals verify type

The engine SHALL support a `registry-value-equals` verify type that compares a named registry value's DATA against an expected value, with an optional value-type assertion.

#### Scenario: Value matches

- **GIVEN** a verify entry with `type: "registry-value-equals"`, an HKCU `path`, a `valueName`, and expected `data`
- **AND** the stored value equals the expected data
- **WHEN** verify is executed
- **THEN** the result passes

#### Scenario: Value mismatch

- **GIVEN** a `registry-value-equals` verify entry whose stored value differs from the expected `data`
- **WHEN** verify is executed
- **THEN** the result fails

#### Scenario: Type mismatch

- **GIVEN** a `registry-value-equals` verify entry with a `valueType` that differs from the stored value's type
- **WHEN** verify is executed
- **THEN** the result fails

#### Scenario: Missing value

- **GIVEN** a `registry-value-equals` verify entry whose named value does not exist
- **WHEN** verify is executed
- **THEN** the result fails with a value-not-found message

### Requirement: registryValues capture support

The module capture system SHALL support a `registryValues` field that snapshots specific named values (value-level), without exporting or rewriting co-resident values under the same key.

#### Scenario: Capture snapshots named values

- **GIVEN** a module with `registryValues` entries specifying a `key` and `valueName`
- **AND** the named values exist
- **WHEN** capture is executed
- **THEN** the engine records each value's type and data to a value-level snapshot under the module's capture payload

#### Scenario: Optional missing value

- **GIVEN** a `registryValues` entry where `optional: true` and the value does not exist
- **WHEN** capture is executed
- **THEN** the value is recorded as absent rather than failing the capture

#### Scenario: Required missing value

- **GIVEN** a `registryValues` entry where `optional` is false and the value does not exist
- **WHEN** capture is executed
- **THEN** the capture fails with a value-not-found error

### Requirement: Value-level fields flow through module expansion

The engine SHALL carry the value-level registry fields (`key`, `valueName`, `valueType`, `data`) from a config module's restore and verify definitions into the expanded manifest entries.

#### Scenario: registry-set fields are preserved on expansion

- **GIVEN** a config module with a `registry-set` restore and a `registry-value-equals` verify carrying value-level fields
- **WHEN** the module is expanded into a manifest via `ExpandConfigModules`
- **THEN** the injected restore and verify entries retain the `key`, `valueName`, `valueType`, and `data` fields
