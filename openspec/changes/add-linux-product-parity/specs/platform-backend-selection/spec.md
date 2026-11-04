## MODIFIED Requirements

### Requirement: Package-reference resolution is platform-keyed
Package-reference resolution SHALL prefer the `App.Refs` entry keyed by the host operating system. On the Linux or Darwin Nix-realizer path, an app without a ref for the host SHALL be skipped and SHALL NOT fall back to a Windows, native-manager, Flatpak, Snap, or Brew ref. Existing Windows fallback behavior for legacy manifests SHALL remain backward compatible until a separate migration changes it.

#### Scenario: Windows ref selected on Windows
- **WHEN** an app has a `refs.windows` entry and the host is Windows
- **THEN** `refs.windows` SHALL be used, unchanged from prior behavior

#### Scenario: Linux ref selected on Linux
- **WHEN** an app has a `refs.linux` entry and the host is Linux
- **THEN** `refs.linux` SHALL be used

#### Scenario: Nix path has no host ref
- **WHEN** an app on the Linux or Darwin Nix-realizer path has no ref for the host but has a non-empty ref for another platform or discovery source
- **THEN** the app SHALL be skipped with a stable platform-reference reason
- **AND** no cross-platform or discovery-source ref SHALL be passed to Nix

#### Scenario: Fallback when no OS-keyed ref exists
- **WHEN** an existing Windows manifest has no `refs.windows` entry but has a legacy non-empty package ref accepted by the current Windows resolver
- **THEN** the existing Windows fallback behavior SHALL remain unchanged
- **AND** this compatibility SHALL NOT authorize fallback on the Nix-realizer path

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
