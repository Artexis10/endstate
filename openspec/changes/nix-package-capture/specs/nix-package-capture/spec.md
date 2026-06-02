## ADDED Requirements

### Requirement: Capture works on realizer backends

The engine SHALL support `capture` on a whole-set realizer backend (the Nix realizer on linux/darwin),
not only the winget driver. When the host has a realizer, `capture` SHALL read the installed package
set from the realizer and produce a manifest from it, rather than requiring winget. When the host has
no realizer (e.g. Windows), `capture` SHALL continue to use the winget export path unchanged.

#### Scenario: Capture emits the installed realizer set as a manifest

- **WHEN** `capture` runs on a host with a realizer and the realizer reports a set of installed
  packages
- **THEN** the engine SHALL write a manifest containing one app per installed package
- **AND** the manifest SHALL use the same output shape and output-path resolution as the winget path

#### Scenario: Empty realizer set produces a valid empty manifest

- **WHEN** `capture` runs on a host with a realizer that reports no installed packages
- **THEN** the engine SHALL write a valid manifest with an empty apps list and SHALL NOT error

#### Scenario: A backend infrastructure failure surfaces as an error

- **WHEN** `capture` runs and reading the realizer set fails with a systemic backend failure
  (the backend is unavailable, or permission is denied)
- **THEN** the engine SHALL return a top-level error rather than writing a partial manifest

### Requirement: Captured realizer refs round-trip through apply

The engine SHALL emit, for each captured realizer package, a host-keyed reference (keyed by the host
operating system) that `apply` can consume to re-install the same package. A `capture` followed by an
`apply` of the produced manifest SHALL converge to the same installed set. The emitted reference SHALL
NOT embed host-architecture detail, so a manifest captured on one operating system remains usable on
another supported realizer host.

#### Scenario: Each captured package carries a host-keyed reference

- **WHEN** `capture` emits a package from the realizer set
- **THEN** the app's refs SHALL contain an entry keyed by the host operating system
- **AND** that reference SHALL be the one `apply` resolves to re-install the same package

#### Scenario: Capture then apply converges to the same set

- **WHEN** a manifest produced by `capture` from a realizer set is applied on a realizer host
- **THEN** the installed set after apply SHALL match the set that was captured

#### Scenario: Updating an existing manifest does not duplicate packages

- **WHEN** `capture` updates an existing manifest and a captured package is already present in it
  under the host key
- **THEN** the engine SHALL NOT add a duplicate entry for that package

### Requirement: Realizer capture is package-scoped

Realizer capture SHALL capture installed packages only. It SHALL NOT synthesize config modules,
manual-app entries, or a configuration bundle on the realizer path; those belong to the winget app
catalog. Configuration/settings capture is out of scope for realizer capture.

#### Scenario: No config modules on the realizer path

- **WHEN** `capture` runs on a realizer host
- **THEN** the produced result SHALL contain no config-module entries and no configuration bundle
- **AND** the manifest SHALL contain only package apps
