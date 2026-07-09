## ADDED Requirements

### Requirement: Capabilities advertises ifChanged support

The capabilities envelope SHALL include `features.hostedBackup.ifChanged` as an explicit boolean so the GUI has a contract-backed gate for the conditional auto-backup flow (`--if-changed` skips upload when the manifest hash is unchanged).

#### Scenario: Capabilities advertises ifChanged
- **WHEN** a client invokes `capabilities --json`
- **THEN** `features.hostedBackup.ifChanged` is `true`

### Requirement: ifChanged is the canonical GUI gate for conditional auto-backup

The GUI SHALL gate its conditional auto-backup flow on `features.hostedBackup.ifChanged` rather than probing the `commands.backup.flags` list. This is documented in `docs/contracts/gui-integration-contract.md`.

#### Scenario: GUI consumes the capability gate
- **WHEN** a client reads the capabilities response
- **THEN** it uses `features.hostedBackup.ifChanged === true` to decide whether to pass `--if-changed` to `backup push`
- **AND** it does NOT rely on the presence of `--if-changed` in `commands.backup.flags` as the gate
