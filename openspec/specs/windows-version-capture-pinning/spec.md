# windows-version-capture-pinning Specification

## Purpose
TBD - created by archiving change windows-version-capture-pinning. Update Purpose after archive.
## Requirements
### Requirement: The winget backend records installed package versions

When recording a Provisioning Generation on the winget backend, the engine SHALL include the
installed version of each package whose version the package manager exposes, so the history reflects
what was actually committed. A version the package manager does not expose SHALL be recorded as
empty rather than failing the run.

#### Scenario: Generation records the installed version of present packages

- **WHEN** `apply` runs on the winget backend and a declared package is already installed
- **THEN** the Provisioning Generation entry for that package SHALL record the installed version
  reported by the package manager

#### Scenario: Missing version does not fail capture

- **WHEN** the package manager exposes no version for a package
- **THEN** the engine SHALL record an empty version for that package
- **AND** the run SHALL NOT fail because a version was unavailable

### Requirement: A declared version pins the installed version

When the manifest declares a version for a package, the engine SHALL install that exact version on a
backend that supports versioned installation, rather than the latest available version.

#### Scenario: Declared version installs that exact version

- **WHEN** `apply` installs a missing package whose manifest entry declares a version, on a backend
  that supports versioned installation
- **THEN** the engine SHALL install the declared version
- **AND** the Provisioning Generation SHALL record that version as the installed version

#### Scenario: Pinning applies only when installing

- **WHEN** a package is already installed at a version different from the declared version
- **THEN** the engine SHALL leave the installed package unchanged (it remains present)
- **AND** the engine SHALL NOT downgrade, upgrade, or reinstall it solely to match the declared
  version

#### Scenario: No declared version installs the latest

- **WHEN** a package's manifest entry declares no version
- **THEN** the engine SHALL install the latest available version, exactly as before

### Requirement: An unavailable pinned version fails the package

The engine SHALL fail a package whose declared version the package manager cannot install, and SHALL
NOT install a different version in its place.

#### Scenario: Unavailable pinned version is an install failure

- **WHEN** `apply` attempts to install a declared version that the package manager does not offer
- **THEN** the engine SHALL report that package as an install failure
- **AND** it SHALL NOT silently install a different version
- **AND** other packages in the run SHALL be unaffected

### Requirement: Version pinning is scoped to the backend that supports it

Version pinning via the manifest's declared version SHALL apply only to backends that install a
specific version (the winget driver). A whole-set realizer that pins exact versions through its own
reference (the Nix realizer) SHALL ignore the declared per-package version, and this SHALL NOT be an
error.

#### Scenario: Realizer backend ignores the declared per-package version

- **WHEN** `apply` runs through the Nix realizer and a package declares a version
- **THEN** the engine SHALL resolve the package through the realizer's reference as usual
- **AND** the declared per-package version SHALL NOT cause an error

