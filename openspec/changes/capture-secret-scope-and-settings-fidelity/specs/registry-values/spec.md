## ADDED Requirements

### Requirement: The catalog loads every module tier

The module catalog SHALL load every directory under `modules/` as a tier, so the `windows-settings` modules load next to the `apps` modules. A module ID SHALL have the form `<tier>.<name>`, for example `windows-settings.explorer`. Modules outside the `apps` tier configure the system, not an application, so the engine SHALL NOT synthesize an app entry for them.

#### Scenario: A Windows settings module loads

- **WHEN** the engine loads the shipped catalog
- **THEN** the catalog contains `windows-settings.explorer`

#### Scenario: A settings module adds no app row

- **WHEN** a manifest declares the config module `windows-settings.explorer`
- **THEN** the engine does not synthesize an app entry for that module

### Requirement: Captured settings bind restore and verify data

When a `registry-set` restore or a `registry-value-equals` verify names a value that its module captures, the bundle SHALL carry the value type and data that capture read on the old PC. A shipped module SHALL NOT carry its own type or data for such an entry, and the release test suite SHALL fail when one does. A bound verify entry SHALL carry `fromModule` with the module ID.

#### Scenario: Restore writes the old PC's value

- **WHEN** capture reads `HideFileExt` as `REG_DWORD` with data `0` for `windows-settings.explorer`
- **THEN** the bundle's `registry-set` restore entry for `HideFileExt` carries `valueType: "REG_DWORD"` and `data: "0"`
- **AND** the bundle's `registry-value-equals` verify entry carries the same type and data and `fromModule: "windows-settings.explorer"`

### Requirement: A value absent at capture is not restored

When a captured value was absent on the old PC, or has a type that `registry-set` cannot write, the bundle SHALL NOT carry a restore or verify entry for that value. Capture SHALL add a plain-language message for each such value to the module's `configModules[].warnings`.

#### Scenario: An unset value stays unchanged

- **WHEN** an optional captured value does not exist on the old PC
- **THEN** the bundle has no `registry-set` entry for that value
- **AND** the module's warnings say that restore leaves the value unchanged

### Requirement: Verify checks a captured setting only after it is restored

Verify SHALL evaluate a verify entry that carries `fromModule` only when the same run restored that registry value; a restore that failed still counts, so verify reports it. Otherwise verify SHALL report the item with status `skipped` and reason `not_restored`, count it in `summary.skipped`, and SHALL NOT report a failure. Standalone `verify` runs no restore, so it SHALL skip every such entry. Verify entries without `fromModule` are unaffected.

#### Scenario: Rebuild restores and then verifies a captured setting

- **WHEN** `rebuild` restores a captured registry value
- **THEN** its verify step compares the value with the captured data

#### Scenario: Standalone verify skips a captured setting

- **WHEN** `verify` runs on a manifest whose verify entry carries `fromModule`
- **THEN** the verify item has status `skipped` and reason `not_restored`
