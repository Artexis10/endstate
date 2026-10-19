## ADDED Requirements

### Requirement: Registry secret coordinates have one reading rule

The engine SHALL read a module's registry secret declarations as follows. A registry coordinate in `secrets.files` or `secrets.registryKeys` SHALL mark both the key at that path with its subkeys and the value named by its last component under the parent key. An entry in `secrets.registryValues` SHALL mark one named value, every value of a key when `valueName` is empty, and the same values in subkeys when `subkeys` is true. The engine SHALL compare keys case-insensitively after it normalizes the hive spelling. A registry-shaped declaration that the engine cannot resolve SHALL make the module's secret boundary unknown.

#### Scenario: A coordinate names a key and a value

- **WHEN** a module declares the secret `HKCU\Software\Vendor\Token`
- **THEN** capture treats the key `HKCU\Software\Vendor\Token` and its subkeys as secret
- **AND** capture treats the value `Token` under `HKCU\Software\Vendor` as secret

#### Scenario: An unresolvable declaration makes the boundary unknown

- **WHEN** a module declares a registry secret that the engine cannot resolve
- **THEN** every registry capture entry of that module conflicts with the secret boundary

### Requirement: Capture filters secrets out of registry data

Capture SHALL remove every declared secret key and value from exported registry data before the data enters the bundle. The engine SHALL parse the filtered data again. When the filtered data does not parse or still contains a secret item, capture SHALL skip that entry as a conflict. `counts.sensitiveExcludedCount` SHALL include each registry key and value that the filter removed; a value inside a removed key counts only as part of that key.

#### Scenario: A secret value inside a captured key is removed

- **WHEN** a module captures `HKCU\Software\Vendor` and declares the value `Token` under that key as secret
- **THEN** the bundle contains the other values of `HKCU\Software\Vendor`
- **AND** the bundle does not contain `Token`
- **AND** `counts.sensitiveExcludedCount` counts the removed value

### Requirement: A conflicting registry entry is skipped, not the module

When a registry capture entry is equal to or inside a declared secret key, names a declared secret value, or cannot be checked against the module's secrets, capture SHALL skip only that entry and SHALL capture the module's other entries. Capture SHALL add one `config_secret_scope_conflict` warning for the module to `warnings[]`, with `moduleId` set to the module ID, and SHALL add the same message to that module's `configModules[].warnings`. The skipped entry SHALL NOT be exported or read.

#### Scenario: One conflicting entry does not fail the module

- **WHEN** a module captures two registry keys and one of them is inside a declared secret key
- **THEN** capture captures the other key
- **AND** `warnings[]` contains one `config_secret_scope_conflict` warning with the module's `moduleId`
- **AND** the module's `configModules[].warnings` contains the same message

### Requirement: Validation mode keeps the strict registry boundary

In the internal validation mode, capture SHALL keep failing a module whose registry capture overlaps its secret boundary, as before this change.

#### Scenario: Validation mode fails on overlap

- **WHEN** a validation-mode capture selects a module whose registry capture overlaps its declared secrets
- **THEN** capture fails before it reads the registry

### Requirement: The shipped catalog is consistent with its secrets

The release test suite SHALL fail when a shipped module's registry capture contradicts its own secret declaration, or when a module file in the catalog does not load.

#### Scenario: A contradicting module blocks the release

- **WHEN** a shipped module captures a registry key that its own secret declaration covers
- **THEN** the catalog integrity test fails
