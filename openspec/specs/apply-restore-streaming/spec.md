# apply-restore-streaming Specification

## Purpose
Defines the NDJSON streaming event contract for restore actions during `apply --EnableRestore`, enabling GUI real-time progress display.

## Requirements
### Requirement: Restore-Item Streaming Events During Apply

The engine SHALL emit `restore-item` NDJSON streaming events during the restore phase of `apply --EnableRestore`, enabling GUI real-time progress display.

#### Scenario: Restore-item events emitted for each restore action

- **WHEN** `apply --EnableRestore --events jsonl` is run with restore entries in the manifest
- **THEN** stderr contains `restore-item` events for each restore entry
- **AND** each event includes: id, module, restorer, source, target, status, reason, backupPath, targetExisted, message
- **AND** restorer is one of: "copy", "merge-json", "merge-ini", "append"
- **AND** status transitions from "restoring" to a terminal status ("restored", "skipped_up_to_date", "skipped_missing_source", "failed")

#### Scenario: Restore phase event emitted between apply and verify

- **WHEN** `apply --EnableRestore --events jsonl` is run with restore entries
- **THEN** stderr contains a phase event with `phase: "restore"` after the apply phase summary and before verify
- **AND** stderr contains a summary event with `phase: "restore"` after all restore actions complete

#### Scenario: No restore events when EnableRestore is not active

- **WHEN** `apply --events jsonl` is run WITHOUT `--EnableRestore`
- **THEN** stderr does NOT contain any `restore-item` events
- **AND** stderr does NOT contain a phase event with `phase: "restore"`

#### Scenario: No restore events when no restore entries exist

- **WHEN** `apply --EnableRestore --events jsonl` is run with a manifest containing zero restore entries
- **THEN** stderr does NOT contain any `restore-item` events
- **AND** stderr does NOT contain a phase event with `phase: "restore"`

#### Scenario: Write-RestoreItemEvent function exists

- **WHEN** `engine/events.ps1` is loaded
- **THEN** the function `Write-RestoreItemEvent` is available
- **AND** it accepts parameters: Id, Module, Restorer, Source, Target, Status, Reason, BackupPath, TargetExisted, Message

#### Scenario: Restore summary includes backupLocation

- **WHEN** restore actions create backups during apply
- **THEN** the restore summary event includes a `backupLocation` field with the backup root directory path
