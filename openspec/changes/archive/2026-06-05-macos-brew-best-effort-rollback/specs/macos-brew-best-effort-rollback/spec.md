## ADDED Requirements

### Requirement: Best-effort brew rollback composes with the native rollback on darwin

When `rollback` runs to an explicit target generation on a host whose realizer owns the native package rollback (Nix on macOS), the engine SHALL also uninstall the union of the brew references added by every provisioning generation whose backend is `brew` and whose number is greater than the target. The native package rollback SHALL run first; the brew uninstall lane SHALL run second and SHALL NOT unwind or abort the native rollback. Each brew uninstall SHALL be best-effort and per package: a per-package failure SHALL be reported and the run SHALL be marked partial, but it SHALL NOT fail while at least one brew uninstall succeeded. The engine SHALL record the brew uninstalls in a separate rollback-marked provisioning generation whose backend is `brew`, and SHALL surface that package-manager-pulled transitive dependencies may remain installed.

#### Scenario: A brew app installed after the target is uninstalled alongside the native rollback

- **WHEN** `rollback --to N --confirm` runs on macOS and a `backend: "brew"` generation numbered greater than N recorded a brew install
- **THEN** the engine SHALL perform the native package rollback to generation N
- **AND** it SHALL uninstall that brew reference through the Homebrew driver
- **AND** it SHALL record the brew uninstall in a separate rollback-marked generation whose backend is `brew`

#### Scenario: A brew uninstall failure does not abort the native rollback

- **WHEN** one brew reference fails to uninstall during a `rollback --to N --confirm` whose native lane already rolled back
- **THEN** the engine SHALL report that reference as failed and mark the run partial
- **AND** the native rollback SHALL stand
- **AND** the run SHALL NOT return a top-level error while another brew uninstall succeeded

#### Scenario: A no-brew history is unchanged from the native-only rollback

- **WHEN** `rollback --to N --confirm` runs and no `backend: "brew"` generation was recorded after N
- **THEN** the engine SHALL produce the same result as the native-only rollback path
- **AND** it SHALL NOT resolve or invoke the Homebrew driver

#### Scenario: Bare rollback leaves brew apps untouched

- **WHEN** `rollback --confirm` runs with no explicit target on a host that has recorded brew generations
- **THEN** the engine SHALL roll back only the native package set, exactly as before this change
- **AND** it SHALL NOT uninstall any brew app

### Requirement: A brew-only rollback target is valid

The engine SHALL accept an explicit `rollback --to N` whose target generation recorded no native package anchor (a `backend: "brew"` generation) when there are brew references to uninstall after it, rolling back the brew packages with no native package rollback. This relaxes the "no native anchor → generation not found" rejection only for the brew-composed case; a target with neither a native anchor, an eligible config rollback, nor brew references to remove SHALL still be rejected.

#### Scenario: A brew-only target rolls back brew packages without a native change

- **WHEN** `rollback --to N --confirm` targets a `backend: "brew"` generation and a later brew generation added references
- **THEN** the engine SHALL uninstall the later brew references
- **AND** it SHALL NOT attempt a native package rollback for that target

### Requirement: Brew rollback requires confirmation, previews under dry-run, and is non-destructive

The engine SHALL require `--confirm` to perform a brew rollback's uninstalls and SHALL refuse without it (the native rollback's existing gate), uninstalling nothing and recording no generation. Under `--dry-run` the engine SHALL report the brew references it would uninstall without uninstalling anything or recording a generation. Cask uninstalls performed by a brew rollback SHALL be non-destructive (never `--zap`), consistent with the backup-before-overwrite and no-silent-deletion posture.

#### Scenario: Dry-run previews brew removals without mutating

- **WHEN** `rollback --to N --dry-run` runs on macOS with brew generations recorded after N
- **THEN** the engine SHALL report the brew references that would be uninstalled
- **AND** it SHALL NOT uninstall any brew app
- **AND** it SHALL NOT record a new provisioning generation

#### Scenario: Without confirmation the brew rollback refuses

- **WHEN** `rollback --to N` runs without `--confirm` and not in dry-run
- **THEN** the engine SHALL refuse with a message naming `--confirm`
- **AND** it SHALL NOT uninstall any brew app
- **AND** it SHALL NOT record a new provisioning generation
