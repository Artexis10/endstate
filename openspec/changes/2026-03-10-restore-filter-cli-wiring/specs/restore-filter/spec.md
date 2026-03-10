## MODIFIED Requirements

### Requirement: RestoreFilter for Per-Module Config Selection

The apply and restore commands SHALL support a --RestoreFilter flag that limits restore execution to specified config modules.

#### Scenario: Capabilities include restore-filter flag

- **WHEN** `capabilities --json` is run
- **THEN** commands.apply.flags includes "--restore-filter"
- **AND** commands.restore.flags includes "--restore-filter"
