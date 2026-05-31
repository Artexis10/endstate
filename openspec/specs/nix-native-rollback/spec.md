# nix-native-rollback Specification

## Purpose
TBD - created by archiving change nix-native-rollback. Update Purpose after archive.
## Requirements
### Requirement: Native package rollback to a prior generation

The engine SHALL provide a top-level `rollback` command that reverts the installed package set to a prior Provisioning Generation, for package backends that advertise native rollback. The target generation SHALL be identified by its engine-owned Provisioning Generation number; the engine SHALL map that number to the backend-native anchor recorded in the generation. Backends that do not advertise native rollback SHALL refuse without changing any state.

#### Scenario: Rollback to a specified generation

- **WHEN** `rollback --to <N> --confirm` runs on a native-rollback backend and Provisioning Generation N exists with a recorded native anchor
- **THEN** the engine SHALL revert the package set to the state recorded by generation N, using that generation's native anchor
- **AND** the user SHALL NOT be required to reference any backend-native version number

#### Scenario: Bare rollback reverts to the previous version

- **WHEN** `rollback --confirm` runs on a native-rollback backend with no target generation specified
- **THEN** the engine SHALL revert the package set to the immediately previous version

#### Scenario: Backend without native rollback refuses

- **WHEN** `rollback` runs on a host whose package backend does not advertise native rollback
- **THEN** the engine SHALL refuse with a stable error code
- **AND** it SHALL NOT install, uninstall, or otherwise modify the installed package set

#### Scenario: Unknown target generation is rejected

- **WHEN** `rollback --to <N>` references a Provisioning Generation number that does not exist
- **THEN** the engine SHALL return a stable error
- **AND** it SHALL NOT modify the installed package set

#### Scenario: Target generation without a native anchor is rejected

- **WHEN** `rollback --to <N>` references a Provisioning Generation that records no backend-native anchor
- **THEN** the engine SHALL return a stable error
- **AND** it SHALL NOT modify the installed package set

### Requirement: Rollback requires explicit confirmation

Because rollback changes the installed package set, it SHALL require an explicit confirmation flag, consistent with non-destructive defaults. A preview mode SHALL be available without confirmation.

#### Scenario: Rollback without confirmation refuses

- **WHEN** `rollback` runs without the confirmation flag and not in preview mode
- **THEN** the engine SHALL refuse and SHALL NOT modify the installed package set
- **AND** it SHALL report how to re-run with confirmation

#### Scenario: Preview does not mutate

- **WHEN** `rollback --dry-run` runs
- **THEN** the engine SHALL report the resolved target without modifying the installed package set
- **AND** it SHALL NOT require the confirmation flag

#### Scenario: Confirmed rollback executes

- **WHEN** `rollback --to <N> --confirm` runs on a native-rollback backend with a valid target
- **THEN** the engine SHALL perform the rollback to that target

### Requirement: Rollback appends a Provisioning Generation

After a successful rollback, the engine SHALL append a new numbered Provisioning Generation that snapshots the now-active package set, so the generation history remains append-only and the newest record always reflects the active set.

#### Scenario: Successful rollback records the now-active set

- **WHEN** a rollback completes successfully
- **THEN** the engine SHALL write a new Provisioning Generation under the resolved state directory
- **AND** its number SHALL be one greater than the highest existing Provisioning Generation number
- **AND** it SHALL record the backend-native anchor of the now-active set

#### Scenario: The appended generation is marked as rollback-produced

- **WHEN** a rollback appends a Provisioning Generation
- **THEN** that generation SHALL be distinguishable as produced by a rollback
- **AND** its added references SHALL be empty, because no package was newly installed

### Requirement: Rollback confines backend diagnostics

Raw backend output (for example, Nix CLI text) produced during a rollback SHALL be confined to the error detail channel and SHALL NOT appear in the user-facing error message.

#### Scenario: Raw backend text is confined to detail

- **WHEN** a rollback fails with backend diagnostic output
- **THEN** the raw backend text SHALL appear only in the error detail
- **AND** the user-facing message SHALL NOT contain the raw backend text

