## ADDED Requirements

### Requirement: Linux discovery and capture do not require backend bootstrap
Read-only ordinary-machine Linux discovery and capture SHALL run without bootstrapping Nix or any native package source. If Nix is absent, its inventory adapter SHALL report unavailable while other discovery adapters continue. Nix installation SHALL remain an apply-time operation governed by the existing explicit consent flags/event contract.

#### Scenario: Capture runs with Nix absent
- **WHEN** Linux capture starts without a Nix executable and has usable native/desktop/config evidence
- **THEN** it SHALL discover and capture supported resolved state
- **AND** it SHALL neither request bootstrap consent nor install Nix

#### Scenario: Apply later needs Nix
- **WHEN** the resulting manifest is applied on a Linux target without Nix
- **THEN** the existing combined backend consent flow SHALL govern Nix bootstrap
- **AND** no package mutation SHALL occur before consent and post-install verification

#### Scenario: Nix inventory source fails but capture continues
- **WHEN** Nix is installed but its read-only inventory probe fails while another trustworthy discovery source succeeds
- **THEN** capture SHALL surface a Nix-source warning and continue with the successful evidence
- **AND** it SHALL NOT attempt repair or reinstallation during capture

### Requirement: Linux release and bootstrap install the complete runtime asset set
A Linux release artifact SHALL carry the engine binary together with the version-matched modules, payload, package catalog, frozen Home Manager registry, and schema-version identity required by discovery, capture, validation, and apply. Unix bootstrap SHALL install or resolve that complete deterministic asset set; a standalone binary that depends on a source checkout or `ENDSTATE_ROOT` SHALL NOT satisfy the ordinary-machine release contract.

#### Scenario: Installed binary discovers runtime catalogs
- **WHEN** the shipped Linux artifact is installed on a clean user account without a source checkout or `ENDSTATE_ROOT`
- **THEN** the engine SHALL resolve its version-matched modules, package catalog, Home Manager registry, payload, and schema identity
- **AND** ordinary discovery SHALL run without repository-relative paths

#### Scenario: Runtime assets do not match the binary
- **WHEN** the installed catalog/schema identity does not match the engine release identity
- **THEN** bootstrap or engine startup SHALL fail with a stable artifact-version error
- **AND** it SHALL NOT silently use stale or ambient repository data

#### Scenario: Supported architectures are smoked from shipped artifacts
- **WHEN** Linux amd64 or arm64 is included in the engine release matrix
- **THEN** required CI SHALL execute bootstrap, catalog lookup, immutable-pin inspection, and no-Nix capture from that architecture's shipped artifact
- **AND** a locally built development binary SHALL NOT substitute for the release smoke
