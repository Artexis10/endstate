# multi-driver-package-management Specification

## Purpose
Defines authoritative multi-driver package routing, lifecycle behavior, capture provenance, warnings, bootstrap integration, and backend-scoped rollback across Winget, Chocolatey, Brew, and the Nix realizer.
## Requirements
### Requirement: Platforms expose a registry of package backends
The engine SHALL model the current platform as one optional whole-set realizer, zero or more named per-package drivers, and an optional default per-package driver. Windows SHALL register `winget` as the default driver and `chocolatey` as an additive driver; macOS SHALL register the Nix realizer and the additive `brew` driver; Linux SHALL register the Nix realizer.

#### Scenario: Windows advertises both package drivers
- **WHEN** capabilities are requested on Windows
- **THEN** `platform.drivers` contains `winget` and `chocolatey`

#### Scenario: Existing Windows manifest uses the default
- **WHEN** a Windows app omits `driver`
- **THEN** the engine routes that app through Winget with behavior compatible with the existing single-driver path

#### Scenario: Nix remains a whole-set realizer
- **WHEN** the backend registry is constructed on Linux or macOS
- **THEN** Nix is exposed as the platform realizer rather than adapted to the per-package `Driver` interface

### Requirement: Explicit app drivers are authoritative
The engine SHALL partition apps by their resolved driver before plan, apply, verify, and rollback. An explicit `app.driver` SHALL select only that named driver, and the engine MUST NOT retry or silently fall back through another driver when the selected driver is unavailable or an operation fails.

#### Scenario: Chocolatey app uses its Windows ref
- **WHEN** a Windows app declares `driver: "chocolatey"` and a non-empty `refs.windows`
- **THEN** every package lifecycle operation uses that ref with the Chocolatey driver

#### Scenario: Explicit unavailable driver does not fall back
- **WHEN** an app explicitly selects Chocolatey and Chocolatey cannot be made available
- **THEN** the app is not attempted through Winget and the result identifies Chocolatey as the failed driver

#### Scenario: Globally unknown driver is rejected
- **WHEN** a manifest app names a driver other than the globally known `winget`, `chocolatey`, or `brew` names
- **THEN** manifest validation fails before package mutation

#### Scenario: Known driver on unsupported host is visible
- **WHEN** a manifest app names a globally known driver that is unsupported on the current host
- **THEN** the app is surfaced as a visible skipped item and is not sent to the host's default backend

### Requirement: Chocolatey supports the package lifecycle
The Chocolatey driver SHALL support presence detection, batch detection with installed versions, latest-version installation, exact-version installation, exact-version convergence including downgrade, installed-package enumeration, and uninstall. Expected per-package failures SHALL use the shared item result model; missing or unusable `choco.exe` SHALL be treated as an infrastructure failure.

#### Scenario: Installed Chocolatey package is a no-op
- **WHEN** apply detects that the selected Chocolatey package is already installed at the required state
- **THEN** it reports `present` without invoking an installation

#### Scenario: Pinned package installs the requested version
- **WHEN** an absent Chocolatey app declares a version
- **THEN** apply requests that exact package version from Chocolatey

#### Scenario: Repin converges a drifted package
- **WHEN** a Chocolatey app is installed at a different version and repinning is enabled
- **THEN** apply invokes Chocolatey's version-convergence path with downgrade support and reports the resulting status

#### Scenario: Rollback does not remove dependencies implicitly
- **WHEN** rollback uninstalls a Chocolatey package added by a later generation
- **THEN** it uninstalls that package without requesting recursive dependency removal

#### Scenario: Reboot-required exit is successful with explicit fact
- **WHEN** Chocolatey returns a documented successful reboot-required exit code
- **THEN** the operation is successful and includes `rebootRequired: true`

### Requirement: Capture enumerates package drivers with provenance
Capture SHALL enumerate each available installed-package driver by default and SHALL accept repeatable `--driver <name>` filters. Captured non-default packages SHALL declare their driver, retain installed version evidence when pin capture is requested, and use the existing host-keyed ref shape.

#### Scenario: Mixed Windows capture preserves provenance
- **WHEN** Winget and Chocolatey are available and capture runs without driver filters
- **THEN** the output includes packages from both drivers and each Chocolatey app declares `driver: "chocolatey"`

#### Scenario: Focused driver capture
- **WHEN** capture is invoked with `--driver chocolatey`
- **THEN** it enumerates Chocolatey packages without adding Winget packages

#### Scenario: Missing optional driver does not destroy capture
- **WHEN** unfiltered Windows capture can enumerate Winget but Chocolatey is absent
- **THEN** capture succeeds with Winget data and emits `optional_driver_unavailable` for Chocolatey

#### Scenario: Explicit missing capture driver fails
- **WHEN** capture is invoked with `--driver chocolatey` and Chocolatey is absent
- **THEN** capture fails with a machine-readable driver-unavailable error instead of returning an empty successful capture

### Requirement: Cross-driver capture identity is conservative
Capture SHALL use `(driver, ref)` as package identity. Display-name, ref, or version similarity across different drivers MUST NOT remove an entry. Case-insensitive exact equality of non-empty display names across drivers SHALL emit `possible_duplicate` while preserving both entries; other fuzzy similarity SHALL emit no automatic conclusion. Colliding manifest app IDs SHALL receive deterministic driver-derived suffixes.

#### Scenario: Same ref text from different drivers remains distinct
- **WHEN** Chocolatey and Winget enumerate the same ref text
- **THEN** capture retains both entries because their driver identities differ

#### Scenario: Equal display names warn without suppression
- **WHEN** entries from different drivers have equal non-empty display names ignoring case
- **THEN** capture preserves both entries and emits `possible_duplicate`

#### Scenario: Merely similar names are not classified
- **WHEN** cross-driver display names are not exactly equal ignoring case
- **THEN** capture preserves both entries without a fuzzy duplicate conclusion

### Requirement: Missing required drivers use the existing backend-bootstrap consent
Apply and rebuild SHALL use the existing `--bootstrap-backends` and `--no-bootstrap` flags for missing package managers. Without answered consent, a required missing Chocolatey lane SHALL emit the existing combined consent request and be visibly skipped; dry-run SHALL report the unavailable lane without installing it.

#### Scenario: Apply refuses implicit Chocolatey installation
- **WHEN** a manifest requires Chocolatey, `choco.exe` is absent, and bootstrap consent is unanswered
- **THEN** apply emits the combined consent request and skips the Chocolatey lane without installing Chocolatey

#### Scenario: Dry-run reports prerequisite
- **WHEN** dry-run plans a Chocolatey app on a host without Chocolatey
- **THEN** the plan reports the missing Chocolatey prerequisite without installing it

#### Scenario: Consented bootstrap continues only after verification
- **WHEN** `--bootstrap-backends` is set and Chocolatey's official bootstrap succeeds
- **THEN** the engine verifies that `choco.exe` is callable before beginning Chocolatey app operations

#### Scenario: Bootstrap failure never reroutes apps
- **WHEN** Chocolatey bootstrap or post-install verification fails
- **THEN** the Chocolatey lane is marked failed with structured diagnostics while independent lanes continue
- **AND** its apps are not sent to Winget

### Requirement: Multi-driver warnings and reboot facts are machine-readable
Apply and capture SHALL expose additive `warnings` entries with `code`, `message`, and optional `driver` and `ref`. Chocolatey reboot-required success SHALL expose `rebootRequired: true` on the affected apply item and streaming item event while retaining a successful status.

#### Scenario: Optional capture driver warning is structured
- **WHEN** unfiltered capture continues without optional Chocolatey
- **THEN** the capture result includes a warning whose code is `optional_driver_unavailable` and driver is `chocolatey`

#### Scenario: Reboot success remains successful
- **WHEN** Chocolatey reports a successful reboot-required exit
- **THEN** the apply item and event report success with `rebootRequired: true`

#### Scenario: Package items expose their selected driver
- **WHEN** apply, plan, or verify returns an app item
- **THEN** that item includes the resolved stable driver name

### Requirement: Configuration modules can match Chocolatey packages
Module definitions SHALL accept `matches.chocolatey` package refs. Engine results SHALL preserve the legacy Winget-only `configModuleMap` and SHALL add a driver-aware `packageModuleMap` keyed as `driver:ref`, with each value an array containing every matching module ID; capture module metadata SHALL expose `chocolateyRefs` additively.

#### Scenario: Chocolatey-installed known app captures settings
- **WHEN** a captured Chocolatey package matches a module's `matches.chocolatey` ref
- **THEN** capture associates that module through `packageModuleMap` and captures its available configuration exactly as for a Winget match

### Requirement: Mixed-backend rollback dispatches by recorded backend
Each per-package driver SHALL write a backend-scoped Provisioning Generation. Explicit-target rollback SHALL group post-target added refs by recorded backend. Bare rollback SHALL select every generation sharing the newest non-rollback generation's `runId`, treating one mixed-driver apply as one rollback unit. Each group SHALL invoke only its backend's uninstaller; unknown or unavailable recorded backends MUST NOT be substituted with the platform default.

#### Scenario: Mixed rollback keeps refs with their manager
- **WHEN** generations after the target contain Winget and Chocolatey additions
- **THEN** rollback sends Winget refs only to Winget and Chocolatey refs only to Chocolatey

#### Scenario: Unavailable recorded backend is partial
- **WHEN** one recorded backend is unavailable but another removes its refs successfully
- **THEN** rollback reports the unavailable backend's refs as failed and preserves the successful removals as a partial result

#### Scenario: Bare rollback reverts one mixed-driver run
- **WHEN** the newest apply wrote Winget and Chocolatey generations with the same `runId` and rollback has no `--to`
- **THEN** rollback targets additions from both generations as one operation

### Requirement: Chocolatey source configuration remains external
The Chocolatey driver SHALL use sources already configured for the local Chocolatey installation. Endstate MUST NOT add, remove, enable, disable, or serialize Chocolatey sources or credentials as part of apply, capture, rebuild, or bootstrap.

#### Scenario: Existing private source is used without capture
- **WHEN** Chocolatey resolves a package from an already configured private source
- **THEN** Endstate permits the operation but does not write the source URL or credentials into the manifest or result artifacts
