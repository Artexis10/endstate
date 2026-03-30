## ADDED Requirements

### Requirement: Go engine emits restore-item streaming events

The Go engine's restore command and restore phase within apply SHALL emit NDJSON item events via the event emitter for each restore entry processed. Events SHALL be emitted after each restore action completes, with status mapped from the restore result. A phase event SHALL be emitted before restore begins, and a summary event SHALL be emitted after all restore actions complete.

#### Scenario: Item events emitted for each restore action in Go engine

- **WHEN** the Go engine runs `restore --enable-restore --events jsonl` or `apply --enable-restore --events jsonl`
- **THEN** stderr contains an item event for each restore entry processed
- **AND** each event includes the restore entry ID, category "restore", and a mapped status

#### Scenario: Status mapping from restore results

- **WHEN** a restore entry completes with status "restored"
- **THEN** the item event has status "restored"
- **WHEN** a restore entry completes with status "skipped_up_to_date"
- **THEN** the item event has status "skipped" with reason "up_to_date"
- **WHEN** a restore entry completes with status "skipped_missing_source"
- **THEN** the item event has status "skipped" with reason "missing_source"
- **WHEN** a restore entry completes with status "failed"
- **THEN** the item event has status "failed" with the error message

#### Scenario: Phase and summary events bracket the restore phase

- **WHEN** the Go engine executes restore with events enabled
- **THEN** a phase event with value "restore" is emitted before processing entries
- **AND** a summary event with phase "restore" is emitted after all entries, including total, success, skipped, and failed counts

#### Scenario: No events when events flag is not set

- **WHEN** the Go engine runs restore without `--events jsonl`
- **THEN** no NDJSON events are emitted to stderr
