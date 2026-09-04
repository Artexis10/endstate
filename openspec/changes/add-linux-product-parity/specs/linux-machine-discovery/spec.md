## ADDED Requirements

### Requirement: Discovery works on an ordinary Linux machine without prior Endstate or Nix state
The engine SHALL discover supported applications and configuration on Linux without requiring an Endstate-managed Nix profile, an Endstate Provisioning Generation, a prior Endstate capture, or an installed Nix executable. Discovery SHALL remain useful when any or all of those inputs are absent.

#### Scenario: Fresh Endstate install on a native-package-managed machine
- **WHEN** Linux capture runs on a machine with supported native package and desktop evidence but no Nix installation and no Endstate state
- **THEN** discovery SHALL return the supported existing applications and settings candidates
- **AND** it SHALL NOT fail solely because Nix or Endstate history is absent

#### Scenario: Existing Endstate state is additive evidence
- **WHEN** an Endstate-managed Nix profile and native Linux evidence both exist
- **THEN** discovery SHALL consider both sources
- **AND** it SHALL NOT restrict the result to the Endstate-managed profile

### Requirement: Linux inventory sources are bounded and read-only
The engine SHALL gather inventory through source-specific, read-only adapters. The release adapter set SHALL cover the Endstate Nix profile, available user Nix profiles, explicitly installed Debian-family packages, explicitly installed RPM-family packages, explicitly installed Arch packages, Flatpak applications, supported Snap applications, XDG desktop entries, and catalog-declared executable/config paths. An unavailable adapter SHALL be reported as unavailable and SHALL NOT be installed or enabled by discovery.

#### Scenario: Missing source command is non-fatal
- **WHEN** an inventory adapter's command or database is absent on the host
- **THEN** discovery SHALL mark that source `not_available`
- **AND** it SHALL continue with the remaining sources without attempting to install the missing source

#### Scenario: Native package inventory is not mutated
- **WHEN** discovery reads apt/dpkg, RPM/DNF, pacman, Flatpak, or Snap evidence
- **THEN** it SHALL execute only the adapter's documented read-only operations
- **AND** it SHALL NOT install, remove, upgrade, pin, enable, or reconfigure packages or repositories

#### Scenario: Source failure is scoped
- **WHEN** one available adapter returns malformed output, permission denial, or a runtime failure and another source succeeds
- **THEN** discovery SHALL return a source-scoped warning and the trustworthy results from the successful source
- **AND** it SHALL NOT silently treat the failed source as empty

### Requirement: Discovery identifies user intent rather than dependency closure
Native-manager adapters SHALL prefer explicitly installed packages, applications, and user-facing desktop evidence over dependency packages, runtimes, SDK subcomponents, and platform bases. The result SHALL expose counts for ignored dependency/runtime items without presenting them as selected applications.

#### Scenario: Debian dependency packages are filtered
- **WHEN** dpkg contains both manually installed applications and packages installed only as dependencies
- **THEN** discovery SHALL consider the manually installed set as package intent
- **AND** dependency-only packages SHALL NOT appear as resolved or unresolved user applications

#### Scenario: Flatpak runtimes are filtered
- **WHEN** Flatpak reports applications and runtime/platform refs
- **THEN** discovery SHALL consider application refs
- **AND** it SHALL count but not present runtimes as user applications

#### Scenario: Desktop evidence marks an app as user-facing
- **WHEN** a reviewed package mapping has both native package evidence and a matching visible XDG desktop entry
- **THEN** discovery SHALL merge that evidence into one application candidate
- **AND** it SHALL retain the desktop evidence as the reason the package is user-facing

### Requirement: Package normalization uses reviewed source-qualified mappings
The engine SHALL normalize non-Nix inventory into a portable application identity only through a versioned, reviewed catalog mapping for that exact source namespace. Each resolved result SHALL preserve the source manager/reference/version evidence separately from the portable Nix attribute and immutable Nixpkgs input. Display-name similarity SHALL NOT establish equivalence.

#### Scenario: Reviewed native alias resolves
- **WHEN** a native package reference exactly matches a reviewed alias for its source namespace
- **THEN** discovery SHALL emit the mapped portable application ID and Nix intent
- **AND** it SHALL preserve the native source reference, installed version, mapping revision, portable attribute, and immutable input as separate fields

#### Scenario: Similar name is not guessed
- **WHEN** an inventory item has no exact reviewed mapping but its name resembles a Nix attribute or another application's display name
- **THEN** discovery SHALL leave the item unresolved with reason `mapping_missing`
- **AND** it SHALL NOT emit an installable app for it

#### Scenario: Ambiguous catalog alias is rejected before runtime
- **WHEN** two catalog entries claim the same alias in the same source namespace
- **THEN** catalog validation SHALL fail
- **AND** release or capture SHALL NOT select either mapping by declaration order

#### Scenario: External installed version is evidence, not desired Nix version
- **WHEN** a resolved native package reports an installed version
- **THEN** discovery SHALL preserve it as source evidence
- **AND** capture SHALL NOT use it as `App.Version` or claim that the release Nixpkgs input provides the identical build

### Requirement: Discovery reconciliation is deterministic and source-qualified
The engine SHALL merge evidence only after it resolves to the same portable application identity. Repeated runs over the same source data SHALL produce the same ordering, IDs, evidence sets, and resolution outcomes. Native references from different source namespaces SHALL remain distinct even when their strings match.

#### Scenario: Multiple sources resolve to one application
- **WHEN** native package, desktop-entry, executable, and config-path evidence all resolve to the same portable application ID
- **THEN** discovery SHALL emit one resolved application with all non-duplicate evidence attached
- **AND** it SHALL select version/reference facts according to documented source precedence

#### Scenario: Same raw ref in different managers stays qualified
- **WHEN** two managers report the same raw package string but the catalog maps them differently or only one is mapped
- **THEN** discovery SHALL keep their evidence source-qualified
- **AND** it SHALL NOT deduplicate them solely by raw string

#### Scenario: Input ordering changes
- **WHEN** adapters return identical evidence in a different order
- **THEN** the serialized discovery result SHALL remain identical

### Requirement: Discovery results distinguish portable, settings-capable, and unresolved state
The capture result SHALL include an additive `discovery` object that reports source status, resolved package intents, settings candidates and supported actions, unresolved user-facing inventory with stable reason codes, and counts for discovered, resolved, selected, unresolved, and ignored items. Existing capture fields SHALL remain populated for resolved selected apps.

#### Scenario: Client can render a useful selection without inspecting Linux
- **WHEN** discovery completes with resolved apps, settings candidates, and unresolved items
- **THEN** the result SHALL contain the stable IDs, display names, evidence summaries, portability state, settings actions, and reason codes needed to render selection
- **AND** a client SHALL NOT need to query package managers, Nix, module files, or host paths itself

#### Scenario: Config-only application remains useful
- **WHEN** catalog-declared configuration exists for an application whose package cannot be resolved
- **THEN** discovery SHALL emit a settings candidate with package portability marked unresolved
- **AND** it SHALL NOT discard the supported settings merely because package mapping is absent

#### Scenario: Primary errors use product language
- **WHEN** a discovery source fails or an item cannot be mapped
- **THEN** the user-facing message SHALL explain the application-source or portability problem without requiring backend knowledge
- **AND** backend names, commands, and raw output SHALL appear only in inspectable details

### Requirement: Discovery and capture are non-destructive and privacy bounded
Discovery and capture SHALL NOT modify packages, repositories, desktop settings, application configuration, Home Manager state, or Endstate provisioning history. Persisted evidence SHALL exclude arbitrary command output and machine-local absolute roots unless an existing governed provenance field explicitly requires a non-secret portable coordinate.

#### Scenario: Capture state is unchanged except for its output
- **WHEN** Linux discovery and capture complete successfully
- **THEN** only the requested Endstate output/artifact and ordinary capture bookkeeping SHALL be written
- **AND** installed packages and user configuration SHALL remain unchanged

#### Scenario: Raw package-manager output is not persisted
- **WHEN** an adapter returns output containing unrelated package or host details
- **THEN** the engine SHALL persist only normalized fields required by the discovery contract
- **AND** it SHALL NOT embed the raw output in the manifest or bundle

### Requirement: Linux product readiness requires the ordinary-machine journey
The engine SHALL NOT advertise ordinary-machine Linux capture as release-ready until required verification demonstrates discovery without Nix, selected capture, Nix/Home Manager or safe-restore apply, package and settings verification, settings revert, package rollback, and release-binary execution from clean state.

#### Scenario: Managed-profile tests alone pass
- **WHEN** tests cover only an Endstate-owned Nix profile or prior Endstate Home Manager history
- **THEN** the ordinary-machine capability SHALL remain unready

#### Scenario: Required fresh-machine journey passes
- **WHEN** the required Linux acceptance job starts from native package/config evidence with no Endstate profile, captures it, applies it into isolated clean state, verifies it, reverts settings, and rolls back packages
- **THEN** the engine capability MAY advertise the ordinary-machine journey as ready
- **AND** the Linux GUI/release gate SHALL still be evaluated by its owning repository
