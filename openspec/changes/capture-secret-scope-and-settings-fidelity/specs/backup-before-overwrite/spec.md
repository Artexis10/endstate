## ADDED Requirements

### Requirement: A preview writes no backup

A dry-run restore SHALL decide its outcome before any backup write and SHALL write no file under the backup root, for every restore strategy. A dry-run restore item SHALL report `backupCreated: false` and no `backupPath`, including a generation-aware item whose prior state the preview inspected in a disposable root.

#### Scenario: A previewed registry import leaves no backup

- **WHEN** a dry run previews a `registry-import` action with `backup: true` whose target key exists
- **THEN** the backup root contains no new file
- **AND** the restore item reports `backupCreated: false`

#### Scenario: A previewed generation-aware restore reports no backup

- **WHEN** a dry run previews a generation-aware config set whose target file exists
- **THEN** its restore item reports `targetExistedBefore: true`, `backupCreated: false`, and no `backupPath`
