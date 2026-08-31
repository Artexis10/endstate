## ADDED Requirements

### Requirement: Module schema v3 declares explicit platform variants
The module loader SHALL accept `moduleSchemaVersion: 3` modules with a `platforms` object keyed by supported host operating systems. Portable module identity and shared presentation metadata SHALL remain common, while each platform variant SHALL own its executable match, detection, capture, generation, restore, verification, secret, path, and realization declarations. Schema-v3 modules SHALL NOT mix platform variants with legacy flat executable declarations.

#### Scenario: One app declares Windows and Linux variants
- **WHEN** a schema-v3 module declares `platforms.windows` and `platforms.linux`
- **THEN** the engine SHALL expose one portable module ID with two explicit platform variants
- **AND** each host SHALL load only the executable declaration for its own platform

#### Scenario: Unsupported platform is inert
- **WHEN** a schema-v3 module has no variant for the host platform
- **THEN** the module SHALL perform no detection, capture, restore, or verification on that host

#### Scenario: Mixed flat and platform declarations are rejected
- **WHEN** a schema-v3 module declares both `platforms` and a legacy top-level matcher, capture, restore, verify, secrets, or config block
- **THEN** catalog validation SHALL reject the module with a structured schema error

### Requirement: Existing flat modules remain valid and Windows-scoped
Existing schema-v1 and schema-v2 modules SHALL continue to load without modification and SHALL be interpreted as Windows variants only. They SHALL NOT run on Linux or Darwin unless migrated to schema v3 with an explicit variant.

#### Scenario: Existing Windows catalog loads unchanged
- **WHEN** the engine loads an existing flat schema-v1 or schema-v2 module on Windows
- **THEN** its matching, capture, restore, verification, secret, and generation behavior SHALL remain unchanged

#### Scenario: Windows path does not accidentally match on Linux
- **WHEN** a flat legacy module contains a path that happens to exist or expand on a Linux host
- **THEN** the module SHALL remain inert because its implicit platform is Windows

### Requirement: Platform qualifies configuration generation identity and provenance
For a schema-v3 module, generation identity SHALL be `<moduleId>/<platform>/<configSetId>/<generationId>`. The engine SHALL include the platform variant's canonical definition in generation fingerprints and module provenance, and SHALL require non-empty source-platform evidence for every schema-v3 capture.

#### Scenario: Same generation names on two platforms are distinct
- **WHEN** Windows and Linux variants both declare config set `preferences` generation `g1`
- **THEN** the engine SHALL treat the generations as distinct platform-qualified identities
- **AND** their fingerprints SHALL be computed from their own variant definitions

#### Scenario: Missing source platform is invalid
- **WHEN** a schema-v3 config capture omits its source platform or variant evidence
- **THEN** manifest/provenance validation SHALL reject the capture
- **AND** it SHALL NOT reinterpret it as a legacy unverified capture

### Requirement: Platform paths use engine-owned coordinates
Schema-v3 module paths SHALL use documented engine-owned coordinates for the user home, XDG config/data/state/cache roots, and other supported platform roots. The engine SHALL supply documented XDG defaults when an environment variable is absent, canonicalize the result, and apply traversal, containment, symlink, and secret validation after expansion. Authored shell evaluation SHALL NOT be supported.

#### Scenario: XDG config default is used
- **WHEN** a Linux module references the XDG config coordinate and `XDG_CONFIG_HOME` is unset
- **THEN** the engine SHALL resolve it beneath `$HOME/.config`
- **AND** the module author SHALL NOT need to embed shell fallback syntax

#### Scenario: Custom XDG root is honored
- **WHEN** `XDG_CONFIG_HOME` is set to an absolute user-owned directory
- **THEN** the engine SHALL resolve the coordinate to that directory
- **AND** it SHALL apply the same host-path safety checks as the default

#### Scenario: Shell expression is rejected
- **WHEN** a schema-v3 path contains command substitution or unsupported shell fallback syntax
- **THEN** catalog validation SHALL reject it rather than executing or partially expanding it

### Requirement: Live settings capture does not require prior Endstate or Home Manager history
An explicit Linux platform variant SHALL be able to detect and capture its supported live configuration from the ordinary user environment even when Endstate did not install the application, create the files, activate Home Manager, or record a Provisioning Generation. Capture SHALL use the platform variant's current trusted rules and SHALL preserve the required source-generation/platform provenance.

#### Scenario: Existing dotfile is captured on a fresh Endstate install
- **WHEN** a supported Linux application config exists at a declared coordinate and no Endstate history exists
- **THEN** the module SHALL be offered as a settings candidate
- **AND** selected capture SHALL collect the allowed payload with platform-qualified provenance

#### Scenario: Home Manager history remains useful but not required
- **WHEN** prior Endstate Home Manager history identifies typed settings and live module capture also finds supported opaque config
- **THEN** capture SHALL reconcile both without requiring either one to replace the other
- **AND** it SHALL not duplicate ownership of the same declared target

### Requirement: Every platform variant declares its settings realization strategy
Each executable platform variant SHALL explicitly declare `home-manager` or `endstate-restore` as its realization strategy. `home-manager` SHALL compile typed settings or captured file placements into an inspectable Endstate-generated Home Manager module. `endstate-restore` SHALL use the existing explicit-consent, backup, validation, journal, and revert path. The engine SHALL NOT choose a strategy from file extension or host state.

#### Scenario: Home Manager variant activates declaratively
- **WHEN** a selected Linux variant declares `home-manager`, configuration changes are enabled, and Endstate owns or may safely create the generated Home Manager activation
- **THEN** apply SHALL stage the declared settings into the inspectable generated configuration
- **AND** it SHALL record the resulting Home Manager generation

#### Scenario: Opaque config uses safe restore
- **WHEN** a selected Linux variant declares `endstate-restore`
- **THEN** apply SHALL require the existing restore consent
- **AND** it SHALL back up prior targets, validate the staged result, record the journal, verify, and support explicit revert

#### Scenario: Strategy is not inferred
- **WHEN** a platform variant omits its realization strategy
- **THEN** catalog validation SHALL reject the executable settings lane

### Requirement: User-owned Home Manager authority is not silently replaced
The engine SHALL classify a live Home Manager activation as externally owned when it exists without matching Endstate provisioning provenance. It SHALL NOT edit, compose into, or replace that configuration automatically. For an externally owned target, a `home-manager` variant SHALL produce an inspectable importable artifact or use only an explicitly declared safe `endstate-restore` fallback after consent.

#### Scenario: External Home Manager activation exists
- **WHEN** apply targets a user with a live Home Manager activation that Endstate history does not own
- **THEN** Endstate SHALL NOT run a generated Home Manager switch that replaces it
- **AND** it SHALL return an ownership action with the generated importable artifact or supported fallback

#### Scenario: Endstate-owned activation can advance
- **WHEN** the active Home Manager generation is linked to Endstate provisioning provenance
- **THEN** apply MAY advance the Endstate-generated configuration after normal plan/consent checks
- **AND** it SHALL record the new generation and effective input revisions

### Requirement: Cross-platform restore requires authored compatibility
A capture SHALL default to restoration through the same platform variant. Restoring to a different platform SHALL require an authored target mapping or migration that names the source and target variants, defines how the payload is interpreted, and validates the target result. Similar filenames, config-set IDs, or generation IDs SHALL NOT imply portability.

#### Scenario: Same-platform restore resolves normally
- **WHEN** a Linux capture is applied to a compatible Linux target variant
- **THEN** normal generation and provenance resolution SHALL apply

#### Scenario: Cross-platform mapping is absent
- **WHEN** a Linux capture is applied on Windows or Darwin and no authored cross-platform mapping exists
- **THEN** the config set SHALL be skipped with reason `platform_mapping_missing`
- **AND** no target configuration SHALL be written

#### Scenario: Authored cross-platform mapping validates
- **WHEN** a module declares and validates a Linux-to-Darwin mapping for a portable config set
- **THEN** the engine MAY materialize the target variant through that mapping
- **AND** it SHALL report both source and target variant identities

### Requirement: Platform variants retain the full secret and safety boundary
Platform-specific secret exclusions SHALL be validated and applied before payload publication or restore. A shared portable module SHALL NOT weaken a stricter variant's exclusions. Credentials, keys, keyrings, authentication stores, histories, caches, databases, and machine-bound state SHALL be excluded unless a separate governed capability explicitly authorizes them.

#### Scenario: Linux credential file shares a config directory
- **WHEN** a supported app stores ordinary settings and an authentication file under the same XDG directory
- **THEN** capture SHALL exclude the authentication file before bundle publication
- **AND** the settings payload SHALL retain a warning when review remains necessary

#### Scenario: Windows and Linux exclusions differ
- **WHEN** two variants store secrets in different locations
- **THEN** each variant's exclusion set SHALL apply independently
- **AND** the common module identity SHALL NOT cause exclusions to be copied or omitted implicitly

### Requirement: Linux application settings coverage has an applicable-Windows parity floor
The Linux release corpus SHALL classify every existing Endstate Windows application module through a deterministic applicability matrix. Every module with a real Linux counterpart SHALL have a verified Linux variant providing equivalent supported settings portability, unless a reviewed platform or safety exclusion explains why equivalent capture is not meaningful or defensible. Unclassified, silently omitted, or capture-only Linux counterparts SHALL block a parity claim when the Windows variant supports safe restore. Safely supported Home Manager programs without a Windows counterpart SHALL be additive Linux coverage.

The release depth corpus SHALL include verified variants for the supported live state of Git, Bash, Zsh, SSH configuration excluding keys, tmux, direnv, Starship, fzf, zoxide, bat, eza, ripgrep, fd, Neovim, Helix, WezTerm, Kitty, Alacritty, GitHub CLI excluding authentication, lazygit, Jujutsu, Atuin excluding account/session material, and Yazi. Each advertised lane SHALL pass capture, restore or declared capture-only behavior, verification, secret-boundary, and revert/rollback tests appropriate to its strategy.

#### Scenario: Applicable Windows module has no Linux disposition
- **WHEN** an existing Windows settings module represents an application available on Linux and the matrix has no supported Linux variant or reviewed exclusion
- **THEN** the Linux settings-parity release gate SHALL fail
- **AND** the module SHALL remain visible in the coverage report rather than being omitted from the denominator

#### Scenario: Home Manager-only program is safely reversible
- **WHEN** the pinned Home Manager corpus contains a program with a reviewed safe Linux capture disposition and no Windows module
- **THEN** it MAY ship as additive Linux settings coverage
- **AND** it SHALL pass the same discovery, round-trip, secret-boundary, and provenance gates as parity modules

#### Scenario: Advertised module lacks live capture evidence
- **WHEN** a corpus module has only a Home Manager emission test or a Windows path test
- **THEN** it SHALL NOT count as Linux live-settings coverage

#### Scenario: Capture-only limitation is visible
- **WHEN** a corpus module can safely capture but cannot yet safely restore
- **THEN** discovery SHALL label the supported action as capture-only
- **AND** the GUI/CLI SHALL NOT offer automatic restore for it

### Requirement: Release-pinned Home Manager metadata is harvested into a frozen adapter registry
The release process SHALL derive candidate Linux adapter metadata from the exact immutable Home Manager input used for realization. The derivation SHALL include machine-readable option metadata, source declarations/hashes, and pure probe evaluation of program-managed home-file targets relative to engine-owned coordinates. The generated registry SHALL be deterministic, reviewed, bundled with the release, and available to runtime discovery without Nix or Home Manager.

Each adapter SHALL declare exactly one reviewed disposition: `typed-roundtrip`, `file-roundtrip`, `curated-codec`, or `excluded`. A source declaration, managed target, option type, or source hash change SHALL invalidate the previous review until the generated diff is accepted. Runtime SHALL NOT infer a capture strategy from a file extension or option name alone.

#### Scenario: Ordinary machine has no Nix installation
- **WHEN** runtime discovery finds a live config target described by the frozen adapter registry on a machine without Nix or Home Manager
- **THEN** it SHALL offer the reviewed settings action using engine-owned path coordinates
- **AND** it SHALL NOT fetch or evaluate Home Manager during capture

#### Scenario: Direct settings mapping has a proven codec
- **WHEN** a Home Manager program writes a reviewed structured settings option directly to a known target and its source-to-target round-trip suite passes
- **THEN** the adapter MAY use `typed-roundtrip` to decode live state and re-emit that settings option
- **AND** the captured payload SHALL preserve only the reviewed portable values

#### Scenario: Generator is not reliably invertible
- **WHEN** a Home Manager module combines defaults, scripts, fragments, migrations, or multiple sources such that typed inversion is not proven
- **THEN** the adapter SHALL use reviewed bounded `file-roundtrip`, a `curated-codec`, or `excluded`
- **AND** it SHALL NOT manufacture typed Home Manager option values from the live file

#### Scenario: Pinned module target changes
- **WHEN** regeneration against a new Home Manager input changes an adapter's declaration hash, option type, or managed target
- **THEN** registry validation SHALL fail the previous review disposition
- **AND** release SHALL require an explicit reviewed update plus the affected round-trip tests

### Requirement: GNOME and KDE settings are value-scoped and reversible
Linux desktop settings modules SHALL operate only on reviewed individual GNOME schema/key values or KDE file/group/key values. Each write SHALL preserve the prior value or absence, verify the desired value, and support explicit revert. Whole dconf databases, whole KDE config directories, account/keyring data, histories, recent items, device topology, and session/window state SHALL NOT be captured or restored.

#### Scenario: GNOME value round-trip
- **WHEN** a selected GNOME module captures and applies one allowlisted schema/key
- **THEN** apply SHALL back up that key's previous value or absence and write only that key
- **AND** verify and revert SHALL compare/restore that same value

#### Scenario: KDE key round-trip
- **WHEN** a selected KDE module captures and applies one allowlisted file/group/key
- **THEN** apply SHALL merge only that key while preserving co-resident values
- **AND** verify and revert SHALL operate on the named value rather than replacing the whole file

#### Scenario: Desktop session is unavailable
- **WHEN** a GNOME or KDE operation requires a usable user session and none is available
- **THEN** the desktop-settings lane SHALL report unavailable with remediation
- **AND** package and independent application-settings capture SHALL continue
