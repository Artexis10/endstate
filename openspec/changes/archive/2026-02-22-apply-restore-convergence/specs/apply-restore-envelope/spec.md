## ADDED Requirements

### Requirement: JSON Envelope Restore Extensions and Journal

The apply command SHALL extend its JSON envelope with `restoreItems[]` and `restoreSummary` when `--EnableRestore` is active, and write a restore journal for revert support.

#### Scenario: restoreItems array in JSON envelope

- **WHEN** `apply --EnableRestore --json` is run with restore entries
- **THEN** the JSON envelope `data` object contains a `restoreItems` array
- **AND** each element includes: id, module, restorer, source, target, status, reason, backupPath, targetExisted, message
- **AND** status is one of: "restored", "skipped_up_to_date", "skipped_missing_source", "failed"

#### Scenario: restoreSummary object in JSON envelope

- **WHEN** `apply --EnableRestore --json` is run with restore entries
- **THEN** the JSON envelope `data` object contains a `restoreSummary` object
- **AND** restoreSummary includes: total, restored, skipped, failed, backupLocation

#### Scenario: Existing items array unchanged

- **WHEN** `apply --EnableRestore --json` is run
- **THEN** the existing `items[]` array contains only app (install) entries
- **AND** restore results are NOT mixed into `items[]`

#### Scenario: No restore fields when EnableRestore not active

- **WHEN** `apply --json` is run WITHOUT `--EnableRestore`
- **THEN** the JSON envelope does NOT contain `restoreItems` or `restoreSummary` fields

#### Scenario: Restore journal written from apply

- **WHEN** `apply --EnableRestore` completes (non-dry-run)
- **THEN** a file `logs/restore-journal-{runId}.json` is written
- **AND** the journal uses the same schema as standalone restore's journal
- **AND** the journal is written even if some restore steps fail

#### Scenario: Apply uses Invoke-RestoreAction for multi-restorer dispatch

- **WHEN** `apply --EnableRestore` processes restore entries
- **THEN** `Invoke-RestoreAction` from `engine/restore.ps1` is used (not `Invoke-CopyRestore`)
- **AND** copy, merge-json, merge-ini, and append restorer types are supported
- **AND** requiresAdmin and requiresClosed checks are enforced
- **AND** exclude patterns are applied

#### Scenario: Consistency across apply paths

- **WHEN** `apply --Plan <plan.json> --EnableRestore --json` is run
- **THEN** the same restore convergence behavior applies as manifest-based apply
- **AND** restoreItems[], restoreSummary, and journal are produced identically
