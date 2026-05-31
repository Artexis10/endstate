## ADDED Requirements

### Requirement: Convergence prunes undeclared packages

The engine SHALL provide an opt-in convergence mode (`apply --prune`) that, after installing the declared packages, removes packages that are installed in the engine-managed package set but are not declared by the manifest — converging the package set to exactly the declared set. Convergence operates only on the engine-managed package set; it SHALL NOT remove system-wide packages the engine did not install.

#### Scenario: Prune removes installed-but-undeclared packages

- **WHEN** `apply --prune --confirm` runs on a backend that supports convergence and the engine-managed package set contains packages not present in the manifest
- **THEN** the engine SHALL uninstall those undeclared packages
- **AND** packages that are declared in the manifest SHALL remain installed

#### Scenario: Prune touches only the engine-managed set

- **WHEN** convergence runs
- **THEN** it SHALL only remove packages from the engine-managed package set
- **AND** it SHALL NOT remove packages the engine did not install

#### Scenario: Default apply never prunes

- **WHEN** `apply` runs without `--prune`
- **THEN** the engine SHALL NOT remove any package
- **AND** undeclared installed packages SHALL be left untouched

### Requirement: Convergence requires explicit confirmation

Because it uninstalls packages, convergence SHALL require an explicit confirmation flag, with a preview available without it.

#### Scenario: Prune without confirmation refuses

- **WHEN** `apply --prune` runs without the confirmation flag and not in preview mode
- **THEN** the engine SHALL refuse to prune and SHALL NOT remove any package
- **AND** it SHALL report how to re-run with confirmation
- **AND** the install phase behavior SHALL be unaffected by the refusal

#### Scenario: Preview lists what would be pruned without mutating

- **WHEN** `apply --prune --dry-run` runs
- **THEN** the engine SHALL report the packages it would prune without removing them
- **AND** it SHALL NOT require the confirmation flag

### Requirement: Convergence is supported only on capable backends

Convergence SHALL be available only on backends that support whole-set removal (the Nix realizer today). A backend that does not support convergence SHALL refuse a prune request with a stable error and change nothing.

#### Scenario: Non-supporting backend refuses to prune

- **WHEN** `apply --prune` runs on a backend that does not support convergence (for example, the winget driver)
- **THEN** the engine SHALL return a stable error
- **AND** it SHALL NOT install, uninstall, or otherwise modify the package set

### Requirement: Convergence records the converged generation

After a convergence that changes the package set, the engine SHALL write a Provisioning Generation that records both the packages added and the packages removed this run, so the history reflects the converged set.

#### Scenario: Converged apply records added and removed refs

- **WHEN** `apply --prune --confirm` installs at least one package and/or removes at least one package
- **THEN** the engine SHALL write a Provisioning Generation
- **AND** it SHALL record the packages installed this run as added references
- **AND** it SHALL record the packages pruned this run as removed references

#### Scenario: No-op convergence writes no generation

- **WHEN** `apply --prune --confirm` neither installs nor removes any package (the set already matches the manifest)
- **THEN** no new Provisioning Generation SHALL be written
