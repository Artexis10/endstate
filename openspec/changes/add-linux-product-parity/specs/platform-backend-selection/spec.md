## ADDED Requirements

### Requirement: Linux discovery sources are separate from mutating package backends
On Linux, Nix SHALL remain the canonical package realizer for apply, verify of desired Nix state, convergence, and rollback. Native package managers, Flatpak, Snap, desktop entries, executables, and config paths MAY be advertised and used as read-only discovery sources, but SHALL NOT become apply backends or fallback mutation paths merely because they supplied capture evidence.

#### Scenario: dpkg evidence produces Nix intent
- **WHEN** discovery maps an explicitly installed dpkg package to a portable application
- **THEN** capture SHALL emit the mapped Nix intent
- **AND** apply SHALL NOT call apt or dpkg to reproduce it

#### Scenario: Nix apply is unavailable
- **WHEN** apply needs a captured Linux package and Nix remains unavailable after the consent/bootstrap decision
- **THEN** the Nix lane SHALL be skipped or fail according to the existing backend-bootstrap contract
- **AND** the engine SHALL NOT silently install through the discovery source manager

#### Scenario: Capabilities distinguish source and backend
- **WHEN** capabilities runs on Linux with dpkg, Flatpak, and Nix available
- **THEN** it SHALL report Nix as the mutating package backend
- **AND** it SHALL report dpkg and Flatpak only in the discovery-source capability
