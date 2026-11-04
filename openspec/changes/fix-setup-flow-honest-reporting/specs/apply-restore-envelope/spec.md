## MODIFIED Requirements

### Requirement: JSON Envelope Restore Extensions and Journal

The apply command SHALL extend its JSON envelope with `restoreItems[]` whenever the manifest carries config payloads, whether or not restore is enabled, and SHALL write a restore journal for revert support when restore is enabled.

The app-result array in the apply envelope is named `actions[]`, matching `docs/contracts/cli-json-contract.md` and the engine's `ApplyResult`. Earlier wording in this spec referred to it as `items[]`; `items[]` is the `generations` command's field and has never existed on the apply envelope.

#### Scenario: restoreItems array in JSON envelope

- **WHEN** `apply --EnableRestore --json` is run with restore entries
- **THEN** the JSON envelope `data` object contains a `restoreItems` array
- **AND** each element includes: id, source, target, status, backupCreated, targetExistedBefore
- **AND** each element includes backupPath when a backup was made, and error, warnings, and restoreType when applicable
- **AND** status is one of: "restored", "skipped_up_to_date", "skipped_missing_source", "failed"

#### Scenario: Existing actions array unchanged

- **WHEN** `apply --EnableRestore --json` is run
- **THEN** the existing `actions[]` array contains only app (install) entries
- **AND** restore results are NOT mixed into `actions[]`

#### Scenario: Apply envelope exposes no items or counts field

- **WHEN** `apply --json` is run in any mode
- **THEN** the envelope `data` object SHALL NOT contain an `items` field
- **AND** SHALL NOT contain a `counts` field
- **AND** app results SHALL be carried by `actions[]` and aggregates by `summary`

#### Scenario: No restore fields without config payloads

- **WHEN** `apply --json` is run with a manifest that carries no config payloads
- **THEN** the JSON envelope does NOT contain `restoreItems`

#### Scenario: Restore journal written from apply

- **WHEN** `apply --EnableRestore` completes (non-dry-run)
- **THEN** a file `logs/restore-journal-{runId}.json` is written
- **AND** the journal uses the same schema as standalone restore's journal
- **AND** the journal is written even if some restore steps fail

#### Scenario: Apply uses restore strategy dispatch for multi-restorer support

- **WHEN** `apply --EnableRestore` processes restore entries
- **THEN** the restore package (`go-engine/internal/restore/`) dispatches each entry by its `type` field via `RunRestore`
- **AND** copy, merge-json, merge-ini, and append restorer types are supported
- **AND** requiresAdmin and requiresClosed checks are enforced
- **AND** exclude patterns are applied
