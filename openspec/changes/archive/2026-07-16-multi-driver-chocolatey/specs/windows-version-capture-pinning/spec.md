## MODIFIED Requirements

### Requirement: The winget backend records installed package versions
When recording a Provisioning Generation for a Windows per-package driver (Winget or Chocolatey), the engine SHALL include the installed version of each package whose version that package manager exposes, so history reflects what was actually committed. A version the package manager does not expose SHALL be recorded as empty rather than failing the run.

#### Scenario: Generation records the installed version of present packages
- **WHEN** `apply` runs on Winget or Chocolatey and a declared package is already installed
- **THEN** that backend's Provisioning Generation entry SHALL record the installed version reported by the package manager

#### Scenario: Missing version does not fail capture
- **WHEN** the selected package manager exposes no version for a package
- **THEN** the engine SHALL record an empty version for that package
- **AND** the run SHALL NOT fail because a version was unavailable

### Requirement: Version pinning is scoped to the backend that supports it
Version pinning via the manifest's declared version SHALL apply only to backends that install a specific version, including Winget and Chocolatey. A whole-set realizer that pins exact versions through its own reference (the Nix realizer) SHALL ignore the declared per-package version, and this SHALL NOT be an error.

#### Scenario: Windows version-capable driver owns the pin
- **WHEN** a missing versioned app is routed to Winget or Chocolatey
- **THEN** the selected driver SHALL install the exact declared version
- **AND** the engine SHALL NOT try the other Windows driver

#### Scenario: Realizer backend ignores the declared per-package version
- **WHEN** `apply` runs through the Nix realizer and a package declares a version
- **THEN** the engine SHALL resolve the package through the realizer's reference as usual
- **AND** the declared per-package version SHALL NOT cause an error

