## ADDED Requirements

### Requirement: Restore journal is written after non-dry-run restore
The Go engine SHALL write a restore journal file at logs/restore-journal-<runId>.json after every non-dry-run restore. The journal SHALL be written even if some restore steps fail. The journal SHALL include: runId, timestamp, manifestPath, manifestDir, exportRoot (nullable), and an entries array. Each entry SHALL include: resolvedSourcePath, targetPath, targetExistedBefore, backupRequested, backupCreated, backupPath (nullable), action, and error (nullable). The journal SHALL use atomic write (temp file + rename).

#### Scenario: Journal written after successful restore
- **WHEN** a non-dry-run restore completes successfully
- **THEN** a journal file is written to logs/restore-journal-<runId>.json with all entry results

#### Scenario: Journal written after partial failure
- **WHEN** a non-dry-run restore has some entries that fail
- **THEN** a journal file is still written containing both successful and failed entry results

#### Scenario: No journal for dry-run
- **WHEN** a restore runs with DryRun=true
- **THEN** no journal file is written

### Requirement: Journal-based revert processes entries in reverse order
The Go engine SHALL implement a revert command that reads the latest restore journal and processes entries in reverse order. For each entry: if a backup exists, restore the backup to target; else if the target did not exist before restore and was restored, delete the created target; else no operation. Skipped_up_to_date entries SHALL NOT be reverted. If no journal exists, revert SHALL report that nothing can be reverted.

#### Scenario: Revert restores from backup
- **WHEN** a journal entry has backupCreated=true and backupPath is set
- **THEN** revert copies the backup back to the target path

#### Scenario: Revert deletes created file
- **WHEN** a journal entry has targetExistedBefore=false and action="restored"
- **THEN** revert deletes the target file that was created by restore

#### Scenario: Revert with no journal
- **WHEN** no restore journal exists in the logs directory
- **THEN** the revert command reports that no journal was found and nothing can be reverted

#### Scenario: Revert processes in reverse order
- **WHEN** a journal has entries [A, B, C]
- **THEN** revert processes C first, then B, then A

### Requirement: Find latest journal
The Go engine SHALL provide a function to find the most recent restore-journal-*.json file in the logs directory, sorted by filename (which includes runId with timestamp).

#### Scenario: Multiple journals exist
- **WHEN** the logs directory contains restore-journal-restore-20260101.json and restore-journal-restore-20260102.json
- **THEN** FindLatestJournal returns the 20260102 journal
