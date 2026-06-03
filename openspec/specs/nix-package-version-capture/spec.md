# nix-package-version-capture Specification

## Purpose
TBD - created by archiving change nix-package-version-capture. Update Purpose after archive.
## Requirements
### Requirement: The Nix realizer capture path records the installed package version

When `capture` runs on a host with a Nix realizer, the engine SHALL parse the installed version
from each element's store paths and record it in the captured manifest app's `version` field,
so a re-apply of the captured manifest can pin the exact version. A store path that does not
yield a parseable version SHALL result in an empty version field for that app, and the run SHALL
NOT fail.

#### Scenario: Captured manifest app carries the parsed store-path version

- **WHEN** `capture` runs on a realizer host and an element's store paths encode a version
- **THEN** the captured manifest app for that element SHALL have its `version` field set to the
  parsed version string

#### Scenario: Unparseable store path does not fail capture

- **WHEN** `capture` runs on a realizer host and an element's store paths do not yield a
  parseable version
- **THEN** the engine SHALL record an empty version for that element
- **AND** the run SHALL NOT fail because a version was unavailable

### Requirement: The Nix realizer apply path records the installed version in the Provisioning Generation

The engine SHALL include the installed version of each package in the Provisioning Generation
when `apply` runs on the Nix realizer path, by parsing the version from each element's store
paths — matching the version-capture behavior of the winget backend. A store path that does not
yield a parseable version SHALL result in an empty version in the generation entry, and the run
SHALL NOT fail.

#### Scenario: Provisioning Generation records the installed version for new installs

- **WHEN** `apply` runs on the Nix realizer path and a package is newly installed
- **THEN** the Provisioning Generation entry for that package SHALL record the parsed store-path
  version (or empty if unparseable)

#### Scenario: Provisioning Generation records the installed version for present packages

- **WHEN** `apply` runs on the Nix realizer path and a package is already present
- **THEN** the Provisioning Generation entry for that package SHALL record the parsed store-path
  version (or empty if unparseable)

#### Scenario: Missing store paths do not fail apply

- **WHEN** `apply` runs and an element has no store paths or an unparseable store path
- **THEN** the engine SHALL record an empty version for that package
- **AND** the run SHALL NOT fail because a version was unavailable

### Requirement: Store-path version parsing is robust and best-effort

The store-path version parser SHALL handle common Nix store path shapes without failing: simple
versions, versions with output suffixes (`-bin`, `-man`, `-dev`, `-doc`, `-lib`), multi-segment
versions, and date-based versions. When the parser cannot extract a version it SHALL return an
empty string rather than raising an error.

#### Scenario: Parser handles output suffixes

- **WHEN** an element's store path encodes a version followed by a known output suffix
  (e.g. `-14.1.0-bin`, `-14.1.0-man`)
- **THEN** the parser SHALL return the version without the suffix

#### Scenario: Parser prefers the exact-name store path

- **WHEN** an element has multiple store paths and one matches the element name exactly
- **THEN** the parser SHALL prefer that path over others when extracting the version

#### Scenario: Parser returns empty on unparseable input

- **WHEN** the store path does not follow the expected `<32hex>-<name>-<version>` format
- **THEN** the parser SHALL return an empty string and SHALL NOT return an error

