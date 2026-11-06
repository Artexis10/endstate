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

#### Scenario: Captured Home Manager files use their declared writer
- **WHEN** apply selects a compatible trusted `home-manager` generation with supported file placements
- **THEN** shared staging SHALL validate its captured payload before the generated Home Manager input consumes it
- **AND** direct restore SHALL NOT copy those targets or create their direct transaction
- **AND** successful result rows SHALL require the actual activation and target-content outcome

#### Scenario: Capture preserves generation authority for either writer
- **WHEN** a selected bound Linux adapter captures settings for either declared strategy
- **THEN** capture SHALL preserve its generation payload, source platform, and immutable module snapshot
- **AND** it SHALL NOT create duplicate ownership through a second serialized file declaration

#### Scenario: Unsupported placement does not infer another strategy
- **WHEN** a captured Home Manager generation contains an operation that its placement bridge cannot represent
- **THEN** that set SHALL report `settings_strategy_unavailable` without direct mutation
- **AND** independent supported settings and package work SHALL remain eligible

#### Scenario: Explicit flake does not inherit captured module ownership
- **WHEN** captured placements require composition into an explicitly declared Home Manager flake
- **THEN** their result rows SHALL report skipped with `home_manager_import_required` and a stable module reference in remediation
- **AND** the engine SHALL NOT edit the flake or label its owner external without observed evidence
- **AND** an independent explicit activation SHALL NOT make those captured rows successful

#### Scenario: Standalone restore cannot substitute a direct writer
- **WHEN** standalone restore selects a captured Home Manager set without an activation backend
- **THEN** it SHALL skip that set with remediation to use apply or rebuild
- **AND** independent direct restore sets SHALL continue normally

### Requirement: Selected settings writers reject known Home Manager target collisions
The engine SHALL include the selected Home Manager activation's targets in its existing concrete-claim collision check alongside direct restore owners. Known exact and parent/child overlaps SHALL prevent both conflicting settings owners from mutating their targets. A blocked Home Manager target SHALL suppress the whole selected activation while preserving unrelated direct restore and package outcomes.

Home preparation SHALL keep target evidence ephemeral and separate from persisted provenance and file-content expectations. Native projection SHALL use enabled, nonrecursive declarations and evaluated target paths from the selected configuration, the frozen lock graph, and a matching activation derivation. Preview SHALL keep nonbuilding preparation and use the same target meaning.

An import-only ownership action SHALL contribute no Home Manager writer. Existing Home Manager symlink leaves SHALL NOT cause a blanket refusal. Recursive declarations SHALL NOT grant ownership of their whole directory. In a real apply, the exact leaves of the built activation package's `home-files` tree SHALL replace evaluated targets as Home Manager claims, which covers recursive declarations and activation-only inputs. Each leaf SHALL map to the same relative path below the home directory that activation inherits, and a link leaf SHALL be one placement, never a claim on its referent. When another selected owner has a file or desktop claim and the built tree cannot be read or exceeds its entry bound, the activation SHALL fail with `target_detection_failed` before any settings writer runs, and direct restores and package outcomes SHALL continue. Preview SHALL NOT build; when recursive declarations or missing parent configuration metadata leave its evaluated claims incomplete, preview SHALL add the `home_manager_collision_check_incomplete` warning and SHALL NOT present them as a complete inventory or add a refusal.

#### Scenario: Direct restore overlaps a selected Home Manager target
- **WHEN** direct restore and a selected Home Manager activation have known overlapping flat targets
- **THEN** neither conflicting settings owner SHALL mutate those targets
- **AND** the command SHALL report `target_collision`
- **AND** unrelated direct restores and committed package outcomes SHALL remain available

#### Scenario: Preview uses the evaluated Home Manager placement
- **WHEN** preview compares a direct target with a selected Home Manager flat target
- **THEN** it SHALL use the evaluated Home Manager directory rather than process XDG variables
- **AND** it SHALL show the same collision without building or presenting that activation as eligible

#### Scenario: Direct restore overlaps a recursive Home Manager leaf
- **WHEN** a real apply selects a Home Manager activation whose recursive declaration places a file that a direct restore also targets
- **THEN** the built activation's leaves SHALL include that file before any settings writer runs
- **AND** neither conflicting owner SHALL mutate it, and the command SHALL report `target_collision`

#### Scenario: Captured placement inside an explicit recursive root
- **WHEN** a real apply composes a captured placement with an explicit configuration whose recursive declaration places the same target
- **THEN** the captured rows SHALL report `target_collision` and the composed activation SHALL NOT run

#### Scenario: Activation-only input in apply
- **WHEN** a real apply selects a Home Manager input without parent configuration metadata, and a direct restore targets a file that its built activation places
- **THEN** neither owner SHALL mutate that file, and the command SHALL report `target_collision`

#### Scenario: Built tree cannot be read
- **WHEN** another selected owner has a file claim and the built tree cannot be read or exceeds its bound
- **THEN** the activation SHALL fail with `target_detection_failed`, and direct restores and packages SHALL continue

#### Scenario: Preview cannot see recursive leaves
- **WHEN** preview selects a Home Manager input with recursive declarations or without parent configuration metadata
- **THEN** it SHALL report the evaluated flat claims and add the `home_manager_collision_check_incomplete` warning
- **AND** it SHALL NOT build or refuse the activation for that reason

#### Scenario: A legitimate Home Manager leaf is a symlink
- **WHEN** an existing Home Manager symlink leaf has no conflicting selected direct writer
- **THEN** its filesystem identity SHALL NOT prevent the Home Manager activation
- **AND** direct-restore safety validation SHALL remain unchanged

#### Scenario: An unrelated direct collision does not select Home Manager
- **WHEN** two direct owners collide and a selected Home Manager activation has disjoint known targets
- **THEN** their collision SHALL NOT suppress that Home Manager activation

#### Scenario: External import contributes no active writer
- **WHEN** Home Manager preparation returns an import action
- **THEN** an import action SHALL remain inactive and SHALL NOT block independent direct restores

#### Scenario: Unavailable advanced projection preserves existing behavior
- **WHEN** preview cannot obtain optional parent configuration metadata
- **THEN** unavailable projection SHALL NOT add a refusal or claim a complete target inventory

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

The target variant SHALL own each one-step mapping. Its `from` and `to`
endpoints SHALL name the platform, config set, and generation. The mapping
SHALL explicitly accept the verified source fingerprint. Its `operations`
array SHALL reuse existing migration operations; an explicit empty array
declares an unchanged payload representation. Nonempty `validate` declarations
SHALL check staging and committed target state through target restore paths.
The mapping SHALL end at the selected target generation without route search.
Each mapping validation SHALL identify exactly one non-excluded, transferred
restore path. A pre-existing target SHALL NOT substitute for transferred state.

The plan SHALL pin the target mapping and target set from the current catalog.
An explicit target instance SHALL NOT supply mapping authority. Existing
`configSetId` SHALL retain source identity. Additive `targetConfigSetId` SHALL
identify a renamed target set. Target collision checks SHALL use target
identity. Reports, events, and journals SHALL retain both platform identities
and the mapping identity while preserving old journal interpretation.

Inbound mappings SHALL remain in canonical snapshots and module revisions but
SHALL NOT affect generation fingerprints. Target secret declarations, scoped
capture exclusions, and restore exclusions SHALL apply before mutation. A
mapping SHALL NOT turn source capture success into target safety authority.

A positive mapping from a legacy schema-v2 capture SHALL require exact source
snapshot linkage for its module, set, generation, and recomputed fingerprint.
That source remains implicitly Windows-scoped. The engine SHALL NOT apply
this stronger mapping precondition to existing same-platform legacy reads.

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

#### Scenario: Equal local generation names still use the mapping
- **WHEN** source and target variants both call their generation `g1`
- **AND** the current target variant accepts the source through an authored mapping
- **THEN** the engine SHALL execute and record that mapping
- **AND** it SHALL NOT treat equal local IDs as same-platform direct restore

#### Scenario: A mapping introduces a target-forbidden member
- **WHEN** valid source payload or a mapping operation creates a member forbidden by target path policy
- **THEN** the engine SHALL fail the mapped set before backups or target writes
- **AND** a capture exclusion SHALL retain its declared source-root scope

#### Scenario: A target excludes a complete capture root
- **WHEN** a target capture exclusion matches its declared source basename
- **THEN** the mapping SHALL treat every transferred member under that source as forbidden
- **AND** the exclusion SHALL NOT apply to a sibling source

#### Scenario: A secret pattern combines recursive and filename wildcards
- **WHEN** target policy forbids a resolved pattern ending in `app/**/token*.json`
- **AND** a mapped member targets `app/nested/token1.json`
- **THEN** the engine SHALL reject that mapped set before mutation
- **AND** it SHALL NOT treat the filename wildcard as literal text

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
The Linux release corpus SHALL classify every existing Endstate Windows application module through a deterministic applicability matrix backed by persistent reviewed applicability authority. Every module with a real Linux counterpart SHALL have a verified Linux variant providing equivalent supported settings portability, unless a reviewed platform or safety exclusion explains why equivalent capture is not meaningful or defensible. Unclassified, silently omitted, or capture-only Linux counterparts SHALL block a parity claim when the Windows variant supports safe restore. Safely supported Home Manager programs without a Windows counterpart SHALL be additive Linux coverage. Release validation SHALL invoke the readiness gate rather than relying on an optional report command.

The release depth corpus SHALL include verified variants for the supported live state of Git, Bash, Zsh, SSH configuration excluding keys, tmux, direnv, Starship, fzf, zoxide, bat, eza, ripgrep, fd, Neovim, Helix, WezTerm, Kitty, Alacritty, GitHub CLI excluding authentication, lazygit, Jujutsu, Atuin excluding account/session material, and Yazi. Each advertised lane SHALL pass capture, restore or declared capture-only behavior, verification, secret-boundary, and revert/rollback tests appropriate to its strategy.

#### Scenario: Applicable Windows module has no Linux disposition
- **WHEN** an existing Windows settings module represents an application available on Linux and the matrix has no supported Linux variant or reviewed exclusion
- **THEN** the Linux settings-parity release gate SHALL fail
- **AND** the module SHALL remain visible in the coverage report rather than being omitted from the denominator

#### Scenario: Readiness is not requested manually
- **WHEN** required release validation evaluates the Linux applicability matrix
- **THEN** it SHALL enforce the same zero-blocker readiness predicate without an optional operator flag
- **AND** a report-only successful exit SHALL NOT satisfy the release gate

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

Each adapter SHALL declare exactly one reviewed disposition: `typed-roundtrip`, `file-roundtrip`, `curated-codec`, or `excluded`. A source declaration, managed target, option type, or source hash change SHALL invalidate the previous review until a bounded deterministic diff is accepted. Runtime SHALL NOT infer a capture strategy from a file extension or option name alone. A non-excluded disposition SHALL remain candidate metadata until a trusted schema-v3 Linux variant binds the adapter to its package/module identity, targets, realization strategy, actions, secret boundary, and lifecycle tests.

#### Scenario: Ordinary machine has no Nix installation
- **WHEN** runtime discovery finds a live config target described by the frozen adapter registry on a machine without Nix or Home Manager
- **THEN** it SHALL offer only the actions jointly authorized by the registry and trusted Linux platform variant using engine-owned path coordinates
- **AND** it SHALL NOT fetch or evaluate Home Manager during capture

#### Scenario: Release-time target evaluation stalls or is cancelled
- **WHEN** a pure target probe exceeds its configured positive deadline or its caller cancels the refresh
- **THEN** the harvester SHALL interrupt the evaluation and return a timeout or cancellation failure
- **AND** the refresh SHALL NOT publish updated registry bytes or accept a late successful result

#### Scenario: Registry facts change before explicit review acceptance
- **WHEN** a release check observes changed inputs, docs, programs, tracked options, types, declarations, source hashes, or managed targets
- **THEN** it SHALL report deterministic typed changes with explicit before/after presence and retain the previous review fingerprints
- **AND** only explicit write acceptance of a complete candidate SHALL calculate new fingerprints, validate, and publish it
- **AND** refresh SHALL preserve existing target-derivation rules, curated aliases, dispositions, probe assignments, and independent module bindings

#### Scenario: Refresh stops after confirmed changes
- **WHEN** a tracked option is absent or a required source read or target probe fails
- **THEN** the report SHALL retain confirmed changes and identify the stop location and incomplete observation state
- **AND** confirmed absence SHALL appear as removal while failed reads SHALL remain unavailable, never fabricated empty observations
- **AND** refresh SHALL preserve its input registry and return no publishable candidate

#### Scenario: Registry review output exceeds display bounds
- **WHEN** observed registry changes exceed 128 rows or a displayed metadata value exceeds 256 bytes
- **THEN** comparison SHALL use full values while the rendered report explicitly counts omitted rows and marks shortened values
- **AND** incomplete counts SHALL describe observed changes, without claiming that unread adapters were checked

#### Scenario: Direct settings mapping has a proven codec
- **WHEN** a Home Manager program writes a reviewed structured settings option directly to a known target and its source-to-target round-trip suite passes
- **THEN** the adapter MAY use `typed-roundtrip` to decode live state and re-emit that settings option
- **AND** the captured payload SHALL preserve only the reviewed portable values

#### Scenario: Generator is not reliably invertible
- **WHEN** a Home Manager module combines defaults, scripts, fragments, migrations, or multiple sources such that typed inversion is not proven
- **THEN** the adapter SHALL use reviewed bounded `file-roundtrip`, a `curated-codec`, or `excluded`
- **AND** it SHALL NOT manufacture typed Home Manager option values from the live file

#### Scenario: File can contain commands or secrets
- **WHEN** a managed target can carry executable commands, environment values, credentials, account identifiers, machine paths, or remote-service state
- **THEN** a generic byte-preserving file adapter SHALL NOT qualify as a reviewed safety boundary
- **AND** the target SHALL require a semantic curated codec, a narrower value adapter, capture-only treatment, or `excluded`

#### Scenario: Pinned module target changes
- **WHEN** regeneration against a new Home Manager input changes an adapter's declaration hash, option type, or managed target
- **THEN** registry validation SHALL fail the previous review disposition
- **AND** release SHALL emit a bounded deterministic review diff and require an explicit reviewed update plus the affected round-trip tests

### Requirement: First-class Linux adapters have joint execution authority and content verification
A Linux settings adapter SHALL count as release-supported only when a trusted schema-v3 Linux variant and its reviewed registry entry agree on portable identity, package/module association, targets, realization strategy, supported actions, and exclusions. Registry presence, a legacy Windows-only module, a bounded file size, or a successful Home Manager generation SHALL NOT independently authorize support.

For every advertised restorable adapter, verification SHALL compare the expected portable values or bytes with the live managed targets after activation. Revert SHALL restore the prior values/absence or file state and SHALL verify that result. A generation-number match alone SHALL NOT satisfy target verification.

#### Scenario: Legacy Windows module has a registry adapter
- **WHEN** an application has a legacy Windows-only module and a non-excluded Home Manager registry entry but no schema-v3 Linux variant
- **THEN** the applicability matrix SHALL NOT classify it as supported
- **AND** runtime SHALL NOT infer Linux execution authority from registry presence

#### Scenario: Registry and module claim different targets or strategies
- **WHEN** the registry target/placement conflicts with the schema-v3 Linux variant target or realization strategy
- **THEN** validation SHALL withhold that adapter's execution and fail its release-readiness check
- **AND** neither declaration SHALL win by load order or prevent independent valid runtime discovery and capture

#### Scenario: Managed target drifts within the same Home Manager generation
- **WHEN** the active Home Manager generation still matches Endstate history but a managed target's live content differs from the expected portable content
- **THEN** verification SHALL report settings drift
- **AND** it SHALL NOT report the adapter verified solely from generation identity

#### Scenario: Additive Home Manager-only program is promoted
- **WHEN** a Home Manager program has no Windows module but is proposed as additive Linux coverage
- **THEN** it SHALL receive the same explicit schema-v3 Linux settings authority and lifecycle proof as a parity module
- **AND** additive status SHALL NOT weaken its safety or verification gates

#### Scenario: Reviewed registry entry has no explicit module binding
- **WHEN** discovery observes a registry target without a matching Linux variant binding
- **THEN** it SHALL retain the candidate evidence without capture, restore, verify, or revert actions
- **AND** it SHALL NOT create a private collection plan or count the entry as release-supported
- **AND** presence observation SHALL NOT open, hash, transform, or collect its payload

#### Scenario: One adapter binding becomes stale
- **WHEN** a binding's reviewed fingerprint or named generation no longer matches current authority
- **THEN** the engine SHALL report a bounded diagnostic and withhold that adapter's execution
- **AND** independent valid adapters SHALL remain usable

#### Scenario: Native restore variant supplies captured files
- **WHEN** a bound Linux variant declares `endstate-restore` and the user selects its observed settings
- **THEN** capture SHALL preserve the selected source-generation provenance through `ConfigCaptures`
- **AND** it SHALL NOT also assign those targets to `homeManager.settings.files`

#### Scenario: Captured ripgrep arguments exceed the reviewed portable subset
- **WHEN** a ripgrep configuration contains an unsupported, positional, command-capable, or path-bearing argument
- **THEN** automatic capture and restore SHALL withhold that file and report its unsupported content
- **AND** the engine SHALL NOT silently remove the argument and report equivalent configuration

#### Scenario: Generated Home Manager file source changes after activation
- **WHEN** a source file changes after Endstate stages and activates a generated Home Manager configuration
- **THEN** verification SHALL compare live targets with expectations from the bytes used for activation
- **AND** it SHALL NOT treat the changed source file or generation identity alone as expected content

#### Scenario: Incoming settings violate the trusted generation policy
- **WHEN** a trusted generation declares a rejecting settings codec for a transferred file
- **THEN** restore SHALL validate the staged bytes before host mutation and retain that codec in committed validation
- **AND** a missing optional source SHALL NOT adopt or validate an unrelated pre-existing host file
- **AND** every transferred file SHALL require committed validation

#### Scenario: A semantic content policy replaces an earlier byte-copy generation
- **WHEN** a Linux adapter introduces a narrower semantic policy with a new generation identity
- **THEN** old capture history SHALL remain readable without gaining automatic execution authority
- **AND** restore SHALL require reviewed migration or semantic revalidation before accepting the old payload

### Requirement: Desktop values use the durable configuration transaction model
GNOME and KDE value operations SHALL extend a shared durable configuration transaction model before either desktop adapter is advertised. The model SHALL assign stable typed target identities, snapshot the prior value or absence before mutation, persist intent and journal state atomically, verify commit, classify partial failures, recover interrupted work, and support explicit revert. A standalone desktop command wrapper SHALL NOT bypass this transaction model.

The first implementation SHALL extend the existing configuration transaction with boolean preference `org.gnome.desktop.interface/clock-show-date` at `/org/gnome/desktop/interface/clock-show-date`. Canonical bounded payloads SHALL distinguish explicit absence from `false`, reject unsupported or contradictory state, and keep effective defaults separate from user overrides. Trusted generation declarations SHALL supply target authority; payloads SHALL supply only desired state. An unavailable or schema-filtered wrong-type read SHALL NOT become absence.

The declaration grammar SHALL use a `desktop` instance detector with a `desktop` target, `capture.desktopValues` entries with `target` and `dest`, and `desktop-value` restores with `source` and `desktop`. Each target SHALL declare `schema`, an absolute backing directory `path` ending in `/`, `key`, and `type: "boolean"`. The payload SHALL contain `type`, `present`, and a boolean `value` exactly when present, in that field order with one final newline, within 1024 bytes. It SHALL contain no target or scope authority.

Desktop-bearing journals SHALL use a new format version. Existing filesystem and registry journal encodings, digests, readers, and offline inspection SHALL remain compatible without rewriting history. A private desktop snapshot SHALL bind the resolved backend/profile namespace and preserve case-sensitive target identity. Recovery SHALL refuse an unrelated third value or a changed runtime scope rather than overwrite it.

#### Scenario: Prior value is inherited rather than user-set
- **WHEN** a GNOME key has an effective default but no user override before apply
- **THEN** the transaction snapshot SHALL record prior absence distinctly from the effective value
- **AND** revert SHALL reset the user override rather than writing the inherited default as a new override

#### Scenario: KDE key is absent beside co-resident values
- **WHEN** a KDE file/group lacks the selected key but contains other values
- **THEN** apply SHALL journal prior absence and merge only the selected key
- **AND** revert SHALL remove only that key while preserving the co-resident values

#### Scenario: Process stops after value mutation
- **WHEN** a desktop value was written but terminal journal state was not persisted
- **THEN** recovery SHALL identify the in-progress typed target and reconcile or revert it deterministically
- **AND** unrelated desktop settings SHALL remain untouched

#### Scenario: Explicit false differs from inheritance
- **WHEN** the selected preference has an explicit user value of `false`
- **THEN** capture and verification SHALL retain that explicit value
- **AND** they SHALL NOT replace it with absence or the effective default

#### Scenario: Another owner changes the pending target
- **WHEN** a pending desktop transaction finds a value that matches neither its prior nor desired state
- **THEN** recovery SHALL report an ownership conflict and preserve that value
- **AND** offline inspection SHALL retain the recorded prior and desired states

#### Scenario: A historical filesystem journal is inspected
- **WHEN** a new engine reads a version-1 filesystem or registry journal
- **THEN** it SHALL verify the existing canonical representation and digest unchanged
- **AND** desktop support SHALL NOT require a desktop session to inspect that journal

### Requirement: GNOME and KDE settings are value-scoped and reversible
Linux desktop settings modules SHALL operate only on reviewed individual GNOME schema/key values or KDE file/group/key values through the durable typed-value transaction model. Each write SHALL preserve the prior value or absence, verify the desired typed value, and support explicit revert. Whole dconf databases, whole KDE config directories, account/keyring data, histories, recent items, device topology, and session/window state SHALL NOT be captured or restored. GNOME and KDE capabilities SHALL remain dark until their exact allowlists, supported value types, version ranges, and current real-session gates pass.

The GNOME boundary SHALL use the maintained `dconf` command for user-value reads, writes, and resets with fixed argument vectors, and `gsettings` only for schema introspection. A read SHALL accept only `true`, `false`, or no value; any other stored value SHALL be unsupported. Writes and resets SHALL use commands that report the writer's failure. The engine SHALL probe its commands, session bus, writer, profile, schema, type, and path explicitly. Missing runtime dependencies SHALL affect only desktop availability. A memory or keyfile backend SHALL NOT establish GNOME/dconf promotion evidence.

The initial native scope SHALL require proven dconf fallback `user-db:user` after checking every supported profile override candidate. Its private identity SHALL bind the user ID and GLib-resolved configuration directory's `dconf/user` namespace. Custom or service profiles SHALL remain unavailable until their actual namespace resolution is implemented and verified. Desktop runtime loading SHALL remain lazy for unrelated reads and historical journals; an unresolved pending desktop mutation SHALL retain the existing recovery stop.

Before it treats an empty read as absence, the engine SHALL establish that the user database is missing, or is a readable regular file, not a symbolic link, with a header that the dconf writer accepts. An unreadable file or a rejected header SHALL report unavailable. The engine SHALL NOT parse database tables. Before each read and mutation, the engine SHALL prove that the actual writer's user ID and resolved configuration directory equal the reader's. Unprovable or mismatched scope SHALL report unavailable without mutation. A writer change during a mutation SHALL fail that mutation and SHALL be reported. Rollback and recovery SHALL prove the writer again before each read and write. Ephemeral identities MAY support this check but SHALL NOT become durable recovery identities.

#### Scenario: The backing database cannot be read
- **WHEN** the user database exists but cannot be read, is not a regular file, or has a header that the dconf writer rejects
- **THEN** inspection SHALL report unavailable instead of an absent key
- **AND** apply SHALL leave the database and unrelated preferences unchanged

#### Scenario: The writer uses another namespace
- **WHEN** the reader and actual writer resolve different user databases
- **THEN** apply SHALL report unavailable before writing any value
- **AND** it SHALL NOT use the reader's snapshot to undo a write in the other database

#### Scenario: A stored value violates the schema
- **WHEN** the reviewed boolean key contains a raw user value of another type
- **THEN** inspection SHALL report unsupported state before mutation
- **AND** apply or revert SHALL NOT treat that value as an absent override

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
