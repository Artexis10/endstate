# version-drift-enforcement Specification

## Purpose
TBD - created by archiving change version-drift-enforcement. Update Purpose after archive.
## Requirements
### Requirement: Verify reports version drift for declared versions

The engine SHALL report a verification failure when a manifest declares a version for a package and
the installed version, as reported by the package manager, differs from it. The mismatch SHALL be
distinguishable from an absent package, and SHALL surface both the installed and the declared
version. Drift SHALL be evaluated only for packages that declare a version.

#### Scenario: Declared version differs from installed

- **WHEN** `verify` runs and a declared package is installed at a version different from the version
  the manifest declares for it
- **THEN** the engine SHALL report that item as a failure distinct from "missing"
- **AND** the result SHALL include the installed version and the declared (expected) version

#### Scenario: Declared version matches installed

- **WHEN** `verify` runs and a declared package is installed at the declared version
- **THEN** the engine SHALL report that item as a pass

#### Scenario: No declared version skips the drift check

- **WHEN** `verify` runs and a package declares no version
- **THEN** the engine SHALL NOT report drift for that package on the basis of its version

#### Scenario: Unknown installed version does not flag drift

- **WHEN** the package manager exposes no installed version for a declared package that is present
- **THEN** the engine SHALL NOT report version drift for that package

### Requirement: Apply converges a drifted version on request

The engine SHALL provide an opt-in mode that reinstalls a package's declared version when the
installed version has drifted from it. Because it reinstalls (and may downgrade), this mode SHALL
require explicit confirmation, with a preview available without it. Default apply SHALL NOT change an
already-installed package's version.

#### Scenario: Opt-in convergence reinstalls the declared version

- **WHEN** apply runs in version-convergence mode with confirmation and a declared package is
  installed at a different version
- **THEN** the engine SHALL reinstall the declared version
- **AND** the recorded generation SHALL reflect the declared version as the installed version

#### Scenario: Convergence without confirmation refuses

- **WHEN** apply runs in version-convergence mode without confirmation and not in preview mode
- **THEN** the engine SHALL refuse to reinstall and SHALL change no package version
- **AND** the install-phase behavior SHALL be unaffected by the refusal

#### Scenario: Preview lists drift without changing anything

- **WHEN** apply runs in version-convergence preview mode
- **THEN** the engine SHALL report the packages it would reinstall to converge their version
- **AND** it SHALL NOT reinstall or require confirmation

#### Scenario: Default apply leaves a drifted version untouched

- **WHEN** apply runs without version-convergence mode and a declared package is installed at a
  different version
- **THEN** the engine SHALL leave the installed package unchanged

### Requirement: Drift detection and convergence are scoped to the backend with per-package versions

Version drift detection and convergence SHALL apply only to backends that install a specific
per-package version (the winget driver). A whole-set realizer that pins exact versions through its
own reference (the Nix realizer) SHALL ignore declared per-package versions for drift, and this
SHALL NOT be an error.

#### Scenario: Realizer backend ignores per-package version drift

- **WHEN** `verify` or `apply` runs through the Nix realizer and a package declares a version
- **THEN** the engine SHALL resolve the package through the realizer's reference as usual
- **AND** it SHALL NOT report version drift or attempt version convergence for that package

