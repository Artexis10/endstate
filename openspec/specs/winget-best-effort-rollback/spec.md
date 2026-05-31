# winget-best-effort-rollback Specification

## Purpose
TBD - created by archiving change winget-best-effort-rollback. Update Purpose after archive.
## Requirements
### Requirement: Best-effort rollback for non-native backends

The engine SHALL extend the `rollback` command to package backends that have no native rollback but can uninstall packages. For such a backend, rollback SHALL revert by uninstalling the packages recorded as added after the target Provisioning Generation — the union of the added references of every generation numbered greater than the target. Eligibility SHALL be discovered by the backend exposing an uninstall capability; a backend that can neither roll back natively nor uninstall SHALL refuse without changing any state.

#### Scenario: Rollback to a generation uninstalls later additions

- **WHEN** `rollback --to <N> --confirm` runs on a non-native backend that can uninstall
- **THEN** the engine SHALL uninstall the union of the added references recorded by every Provisioning Generation numbered greater than N
- **AND** it SHALL NOT uninstall packages recorded at or before generation N

#### Scenario: Bare rollback reverts the most recent generation

- **WHEN** `rollback --confirm` runs with no target on a non-native, uninstall-capable backend
- **THEN** the engine SHALL uninstall the added references recorded by the most recent Provisioning Generation

#### Scenario: Backend that cannot uninstall refuses

- **WHEN** `rollback` runs on a backend with neither native rollback nor an uninstall capability
- **THEN** the engine SHALL refuse with a stable error code
- **AND** it SHALL NOT modify the installed package set

#### Scenario: Unknown target generation is rejected

- **WHEN** `rollback --to <N>` references a Provisioning Generation number that does not exist
- **THEN** the engine SHALL return a stable error
- **AND** it SHALL NOT modify the installed package set

#### Scenario: Nothing to roll back is a no-op

- **WHEN** the target generation is already the most recent (no later additions are recorded)
- **THEN** the engine SHALL report that there is nothing to roll back
- **AND** it SHALL NOT uninstall anything

### Requirement: Best-effort uninstall tolerates per-package failure

Because per-package uninstall is non-atomic, rollback SHALL attempt every targeted package independently and report per-package outcomes rather than aborting on the first failure. A package that is already absent SHALL count as a successful removal.

#### Scenario: Per-package failure does not abort the rollback

- **WHEN** one targeted package fails to uninstall (for example, another installed package still depends on it)
- **THEN** the engine SHALL continue attempting the remaining targeted packages
- **AND** it SHALL report the failed package(s) in the result
- **AND** the result SHALL be marked partial

#### Scenario: Already-absent package is a successful no-op

- **WHEN** a targeted package is already not installed
- **THEN** the engine SHALL treat it as successfully removed
- **AND** it SHALL NOT report an error for that package

#### Scenario: Untracked dependencies are surfaced as a caveat

- **WHEN** a best-effort rollback uninstalls packages
- **THEN** the engine SHALL surface a caveat that package-manager-pulled transitive dependencies and co-installs are not tracked and may remain installed

### Requirement: Best-effort rollback requires explicit confirmation

Because it uninstalls packages, best-effort rollback SHALL require an explicit confirmation flag, with a preview available without it. This realizes the non-destructive-defaults invariant for the uninstall path.

#### Scenario: Rollback without confirmation refuses

- **WHEN** `rollback` runs against an uninstall-capable backend without the confirmation flag and not in preview mode
- **THEN** the engine SHALL refuse and SHALL NOT uninstall anything
- **AND** it SHALL report how to re-run with confirmation

#### Scenario: Preview lists what would be removed without mutating

- **WHEN** `rollback --dry-run` runs against an uninstall-capable backend
- **THEN** the engine SHALL report the packages it would uninstall without uninstalling them
- **AND** it SHALL NOT require the confirmation flag

### Requirement: Best-effort rollback appends a Provisioning Generation

After a best-effort rollback that removed at least one package, the engine SHALL append a new numbered Provisioning Generation marked as rollback-produced, recording the removed references, so the history remains append-only and auditable.

#### Scenario: Rollback records what it removed

- **WHEN** a best-effort rollback successfully removes one or more packages
- **THEN** the engine SHALL append a new Provisioning Generation under the resolved state directory
- **AND** it SHALL mark the generation as rollback-produced
- **AND** it SHALL record the removed references
- **AND** its added references SHALL be empty
- **AND** it SHALL be marked partial when any targeted uninstall failed

