## ADDED Requirements

### Requirement: Capture includes both default WinGet application sources

Windows capture SHALL enumerate the WinGet community repository and Microsoft Store source by default, while excluding unrelated explicit or third-party sources.

#### Scenario: Ordinary capture runs

- **WHEN** the user runs capture without package-source flags
- **THEN** the engine enumerates both source `winget` and source `msstore`
- **AND** it runs the source inventories concurrently under the capture `inventory` stage

#### Scenario: Store source is explicitly excluded

- **WHEN** the user runs capture with `--exclude-store-apps`
- **THEN** the engine does not access source `msstore`
- **AND** it captures source `winget` normally

#### Scenario: Legacy include flag is supplied

- **WHEN** the user runs capture with the deprecated `--include-store-apps` flag
- **THEN** argument parsing succeeds
- **AND** capture retains the same default dual-source behavior

#### Scenario: Compatibility include and explicit exclude flags are both supplied

- **WHEN** the user supplies both `--include-store-apps` and `--exclude-store-apps`
- **THEN** the explicit exclude flag wins
- **AND** the engine does not access source `msstore`

#### Scenario: Third-party or font source is configured

- **WHEN** WinGet has additional configured sources such as `winget-font` or a private repository
- **THEN** default capture does not enumerate those sources

### Requirement: Captured Store source is preserved

The engine SHALL preserve Microsoft Store source identity in the captured manifest and authoritative capture result.

#### Scenario: Store package is captured

- **WHEN** source `msstore` reports an installed package
- **THEN** the manifest app uses driver `winget` and source `msstore`
- **AND** its Windows ref contains the Store package identifier
- **AND** `appsIncluded` reports source `msstore`

#### Scenario: Community package is captured

- **WHEN** source `winget` reports an installed package
- **THEN** the manifest app uses driver `winget`
- **AND** an omitted source on a non-Store package remains equivalent to source `winget`

#### Scenario: Manifest declares unsupported Winget source

- **WHEN** an app with driver `winget` declares a source other than `winget` or `msstore`
- **THEN** manifest validation rejects it with a structured validation error

#### Scenario: Source is declared on a non-Winget driver

- **WHEN** an app with driver `chocolatey` or `brew` declares a WinGet package source
- **THEN** manifest validation rejects it with a structured validation error

#### Scenario: Source spelling is normalized

- **WHEN** a Winget app declares source with surrounding whitespace or mixed case
- **THEN** manifest loading normalizes it to lowercase `winget` or `msstore`

#### Scenario: Explicit source conflicts with Store-ID classifier

- **WHEN** a Winget app declares an explicit valid source and its ref would imply another source by ID pattern
- **THEN** the explicit source wins
- **AND** Store-ID inference is not applied

### Requirement: Store packages route through the Store source

The engine SHALL use the preserved package source for Winget detection, installation, verification, and best-effort uninstall.

#### Scenario: Store app is applied

- **WHEN** apply installs an app with driver `winget` and source `msstore`
- **THEN** the Winget install command targets source `msstore`
- **AND** source and package agreements remain non-interactive

#### Scenario: Store app is detected or verified

- **WHEN** plan or verify checks an app with source `msstore`
- **THEN** detection uses a Store-source inventory
- **AND** an installed Store package is reported present rather than missing

#### Scenario: Store app is removed by best-effort rollback

- **WHEN** best-effort rollback removes an app with source `msstore`
- **THEN** the Winget uninstall operation targets the Store package identity without falling back to the community source

#### Scenario: Legacy profile contains Store ID without source

- **WHEN** a legacy manifest has a recognized Store package ID and omits source
- **THEN** the engine routes it to source `msstore` using the compatibility classifier

### Requirement: Planning identity includes package source

The engine SHALL key Winget planning and detection by driver, normalized source, and package ref rather than ref alone.

#### Scenario: Mixed-source plan contains identical refs

- **WHEN** a manifest contains two Winget apps with the same ref and different explicit sources
- **THEN** plan and verify evaluate them as distinct source-aware coordinates
- **AND** a detection result from one source does not satisfy the app from the other source

#### Scenario: Mixed-source batch detection runs

- **WHEN** plan or verify batches Winget apps from both `winget` and `msstore`
- **THEN** the engine partitions detection by source
- **AND** merges results by source-aware coordinate without ref collisions

### Requirement: Provisioning history preserves package source

The engine SHALL persist source-aware package records for newly applied Winget packages so later best-effort rollback targets the original source.

#### Scenario: Apply installs Store package

- **WHEN** apply installs a package from `msstore`
- **THEN** its provisioning generation includes an `addedPackages` record with the ref and source `msstore`
- **AND** the legacy `addedRefs` array continues to contain the ref for backward compatibility

#### Scenario: Later rollback reads source-aware generation

- **WHEN** best-effort rollback targets a generation with source-aware added-package records
- **THEN** it uninstalls each package through its recorded source
- **AND** the rollback generation records source-aware removed-package history

#### Scenario: Rollback reads legacy generation

- **WHEN** a legacy generation contains only `addedRefs`
- **THEN** rollback resolves Store-format refs through the compatibility classifier
- **AND** resolves other Winget refs to source `winget`

### Requirement: Partial Store-source failure is explicit and non-fatal

Capture SHALL preserve successful selected-source results and emit a machine-readable warning when only the Microsoft Store source is unavailable.

#### Scenario: Community source succeeds and Store source fails

- **WHEN** source `winget` returns a usable inventory and source `msstore` is unavailable, disabled, or blocked
- **THEN** capture succeeds with the community-source apps and artifact
- **AND** the envelope includes warning code `store_source_unavailable`
- **AND** capture does not claim that Store apps were included

#### Scenario: Store source succeeds and community source fails

- **WHEN** source `msstore` returns a usable inventory and source `winget` is unavailable
- **THEN** capture succeeds with the Store-source apps and artifact
- **AND** the envelope includes warning code `winget_source_unavailable`
- **AND** capture does not claim that community-source apps were included

#### Scenario: No selected source yields a usable inventory

- **WHEN** every selected package source fails or the merged selected-source inventory remains empty after the existing retry behavior
- **THEN** capture fails with a structured capture error

#### Scenario: Store source succeeds with zero installed packages

- **WHEN** the `msstore` command succeeds and returns parseable output with zero installed packages
- **THEN** the Store source is treated as successfully queried
- **AND** the engine does not emit `store_source_unavailable`

### Requirement: Capture warnings have a stable machine-readable shape

Source and Store-portability warnings SHALL appear in the capture envelope `warnings` array with fields `code`, `message`, `driver`, and `source`.

#### Scenario: Store source is unavailable

- **WHEN** Store enumeration fails while another selected source produces a non-empty usable inventory
- **THEN** the envelope contains exactly one warning with code `store_source_unavailable`, driver `winget`, and source `msstore`

#### Scenario: Multiple warning conditions apply

- **WHEN** capture has more than one distinct warning condition
- **THEN** each warning is a separate element of the `warnings` array
- **AND** consumers can distinguish them by code and source

### Requirement: Exact cross-source duplicates are deterministic

Capture SHALL prevent duplicate manifest entries and duplicate later install attempts when both default sources report the same normalized package ref.

#### Scenario: Store-format ref appears in both sources

- **WHEN** both `winget` and `msstore` report the same normalized Store-format ref
- **THEN** capture retains only the `msstore` entry

#### Scenario: Non-Store ref appears in both sources

- **WHEN** both `winget` and `msstore` report the same normalized non-Store ref
- **THEN** capture retains only the `winget` entry

#### Scenario: Display names match but refs differ

- **WHEN** entries from different sources share a display name but have different refs
- **THEN** capture retains both entries
- **AND** the existing possible-duplicate warning behavior applies

### Requirement: Store version portability is not overstated

Capture SHALL omit exact version pins for Store packages until the Store source supports reliable exact-version restoration.

#### Scenario: Pinned capture includes Store package

- **WHEN** capture runs with `--pin` and includes an `msstore` package
- **THEN** the Store app is captured without a version field
- **AND** the envelope includes exactly one aggregate warning with code `store_version_unpinned`, driver `winget`, and source `msstore`
- **AND** the warning message reports the number of affected Store packages
- **AND** non-Store packages retain their normal pinning behavior
