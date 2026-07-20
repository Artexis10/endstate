## ADDED Requirements

### Requirement: restoreModulesAvailable Is Scoped To Profile Contents

`restoreModulesAvailable` SHALL list only modules for which the manifest carries restorable payload. It SHALL NOT list every catalog module matching the manifest's app list. A module SHALL appear only when at least one restore entry in the manifest resolves to it.

#### Scenario: Module with restore entries is listed

- **WHEN** `apply --dry-run --json` is run against a manifest whose `restore[]` contains entries resolving to `apps.vscodium`
- **THEN** `restoreModulesAvailable` SHALL contain an entry with `id` `apps.vscodium`

#### Scenario: Catalog module without restore entries is excluded

- **WHEN** `apply --dry-run --json` is run against a manifest listing app `Docker.DockerDesktop` with no restore entry resolving to `apps.docker-desktop`
- **THEN** `restoreModulesAvailable` SHALL NOT contain `apps.docker-desktop`

#### Scenario: Manifest with no restore entries offers no modules

- **WHEN** `apply --dry-run --json` is run against a manifest whose `configModules` and `restore` are both empty
- **THEN** `restoreModulesAvailable` SHALL be omitted from the envelope

#### Scenario: Scoping is independent of installed state

- **WHEN** a manifest carries restore entries resolving to a module whose app is not installed on the host
- **THEN** that module SHALL still appear in `restoreModulesAvailable`
- **AND** scoping SHALL depend only on manifest contents, not host state

### Requirement: Module Resolution Order For Restore Entries

The engine SHALL resolve each restore entry to a module using this precedence, so that profiles captured before module provenance existed continue to resolve correctly:

1. The entry's `fromModule` field, when non-empty.
2. Otherwise, the module ID derived from the entry's `source` path prefix (`./configs/<id>/…` or `./payload/apps/<id>/…`), when that ID matches a module in the loaded catalog.
3. If no entry resolves by (1) or (2), the manifest's declared `configModules[]`.

#### Scenario: fromModule takes precedence

- **WHEN** a restore entry has `fromModule` `apps.obsidian` and a `source` of `./configs/other/file.json`
- **THEN** the entry SHALL resolve to `apps.obsidian`

#### Scenario: Legacy profile resolves by source path

- **WHEN** a manifest's restore entries have no `fromModule` and sources of the form `./configs/vlc/…`
- **AND** `apps.vlc` exists in the loaded module catalog
- **THEN** those entries SHALL resolve to `apps.vlc`

#### Scenario: Derived ID not in catalog is not trusted

- **WHEN** a restore entry has no `fromModule` and a source of `./configs/unknown-thing/file.conf`
- **AND** no module `apps.unknown-thing` exists in the catalog
- **THEN** the entry SHALL NOT resolve to a module by path derivation

#### Scenario: Fallback to declared configModules

- **WHEN** no restore entry resolves by `fromModule` or source-path derivation
- **AND** the manifest declares `configModules` containing `apps.windsurf`
- **THEN** `restoreModulesAvailable` SHALL be derived from `configModules`

#### Scenario: Scoping removing all modules emits a warning

- **WHEN** a manifest has restore entries but none resolves to a catalog module by any tier
- **THEN** `restoreModulesAvailable` SHALL be omitted
- **AND** the envelope SHALL include a warning identifying that restore entries could not be attributed to modules

### Requirement: restoreModulesAvailable Entries Carry An Entry Count

Each entry in `restoreModulesAvailable` SHALL include `entryCount`: the number of restore entries resolved to that module. `entryCount` SHALL be derived from the same resolution that determines membership, so the two cannot disagree.

#### Scenario: entryCount reflects resolved restore entries

- **WHEN** three restore entries resolve to `apps.inkscape`
- **THEN** that module's `entryCount` SHALL be `3`

#### Scenario: entryCount is never zero

- **WHEN** any module appears in `restoreModulesAvailable`
- **THEN** its `entryCount` SHALL be greater than `0`

#### Scenario: entryCount present in both dry-run and real apply

- **WHEN** `apply --json` is run with or without `--dry-run`
- **THEN** every `restoreModulesAvailable` entry SHALL include `entryCount`
