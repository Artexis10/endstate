## MODIFIED Requirements

### Requirement: The Nix realizer capture path records the installed package version
When `capture` reads an installed package directly from a Nix profile, the engine SHALL parse the observed version from the element's store paths and record it in the captured app's `version` field for inspection. Exact reproducibility SHALL come from the captured immutable installable/input provenance, not from the parsed version string. A store path that does not yield a parseable version SHALL result in an empty observed version and SHALL NOT fail capture. For an application discovered through a non-Nix manager, that manager's version SHALL remain source evidence and SHALL NOT be copied into the desired Nix app version.

#### Scenario: Nix profile app carries an observed version and immutable ref
- **WHEN** capture reads a Nix profile element whose store path encodes a version
- **THEN** the captured app SHALL expose the parsed observed version
- **AND** its reproducible desired state SHALL be identified by the immutable Nix installable/input provenance rather than the version string alone

#### Scenario: Unparseable store path does not fail capture
- **WHEN** a Nix profile element's store paths do not yield a parseable version
- **THEN** the engine SHALL record an empty observed version for that element
- **AND** the run SHALL NOT fail because the display/inspection version was unavailable

#### Scenario: External manager version remains evidence
- **WHEN** discovery maps a dpkg, RPM, pacman, Flatpak, or Snap item to a Nix intent
- **THEN** the source manager's installed version SHALL be retained in discovery provenance
- **AND** it SHALL NOT be written as a request that Nix install an allegedly equivalent version

#### Scenario: Source and Nix versions differ
- **WHEN** the release Nixpkgs input resolves a mapped portable app to a version different from the source manager's observed version
- **THEN** capture/apply inspection SHALL show the source evidence and target resolution distinctly
- **AND** it SHALL NOT claim an exact cross-manager version match
