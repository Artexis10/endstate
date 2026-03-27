# backup-before-overwrite Specification

## Purpose
Guarantees that existing files are backed up before any restore operation overwrites them, enabling rollback.

## Requirements
### Requirement: Pre-Overwrite Backup

The system SHALL create a backup of any existing file before a restore operation overwrites it.

#### Scenario: Existing config file is backed up before restore
- **WHEN** `apply --EnableRestore` overwrites an existing config file
- **THEN** the original file is copied to `state/backups/<timestamp>/` before the overwrite
- **AND** the backup preserves the original file's relative path structure

#### Scenario: No backup when target does not exist
- **WHEN** `apply --EnableRestore` writes a config file to a path where no file previously existed
- **THEN** no backup entry is created for that file

#### Scenario: Backup occurs for every overwrite strategy
- **WHEN** a restore entry uses any strategy (copy, merge-json, merge-ini, append)
- **THEN** the existing target file is backed up before the strategy executes
- **AND** the backup is a complete copy, not a diff or partial snapshot
