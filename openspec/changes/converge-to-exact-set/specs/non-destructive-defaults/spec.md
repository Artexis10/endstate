## ADDED Requirements

### Requirement: Convergence is opt-in and confirmed

Removal of undeclared (drift) packages via convergence SHALL require an explicit opt-in flag (`--prune`) AND explicit confirmation (`--confirm`). This is a new, distinct requirement that does not relax the default-safe guarantees: without `--prune`, `apply` removes nothing.

#### Scenario: Default apply removes nothing

- **WHEN** `apply` runs without `--prune`
- **THEN** no package SHALL be uninstalled
- **AND** undeclared installed packages SHALL be left untouched

#### Scenario: Convergence is not inferred from the manifest

- **WHEN** `apply` is run without `--prune` against a manifest that declares fewer packages than are installed
- **THEN** the engine SHALL NOT infer removal intent from the manifest
- **AND** no package SHALL be uninstalled

#### Scenario: Prune requires both opt-in and confirmation

- **WHEN** `apply --prune` runs without `--confirm` and not in preview mode
- **THEN** the engine SHALL NOT uninstall any package
- **AND** removal SHALL proceed only when both `--prune` and `--confirm` are present
