## MODIFIED Requirements

### Requirement: Drift detection and convergence are scoped to the backend with per-package versions
Version drift detection and convergence SHALL apply only to per-package backends that implement specific-version installation (the Winget and Chocolatey drivers). A whole-set realizer that pins exact versions through its own reference (the Nix realizer) SHALL ignore declared per-package versions for drift, and this SHALL NOT be an error. A per-package driver without version-selection support SHALL retain its existing advisory behavior.

#### Scenario: Winget and Chocolatey enforce per-package version drift
- **WHEN** `verify` or confirmed repin runs for a versioned app routed to Winget or Chocolatey
- **THEN** the engine SHALL compare the installed version and use that app's selected driver for convergence

#### Scenario: Realizer backend ignores per-package version drift
- **WHEN** `verify` or `apply` runs through the Nix realizer and a package declares a version
- **THEN** the engine SHALL resolve the package through the realizer's reference as usual
- **AND** it SHALL NOT report version drift or attempt version convergence for that package

