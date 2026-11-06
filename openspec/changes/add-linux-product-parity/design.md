## Context

The Linux substrate is already substantial. The engine selects Nix on Linux, applies packages into an Endstate-owned profile, can bootstrap Nix with consent, compiles an Endstate-native Home Manager catalog, records provisioning generations, verifies state, and exposes native rollback. Release workflows publish Linux binaries and CI exercises the code on Ubuntu.

The capture boundary is much narrower than the product claims imply. `nix.Backend.Current()` lists only `${XDG_STATE_HOME}/endstate/nix-profile`; Home Manager capture recovers only inputs that Endstate previously recorded; package mapping assumes a Nix attribute is already known; and the config-module catalog is structurally Windows-first. A machine whose applications came from apt/dpkg, RPM, pacman, Flatpak, Snap, a user Nix profile, or an ordinary desktop installation therefore produces little or no portable state. The current compatibility documentation calls this platform support, but it is substrate support rather than the normal-person capture/rebuild journey Endstate provides on Windows.

There is also a reproducibility gap in the defaults. Bare Nix attributes currently expand against `nixpkgs`, and the default Home Manager input tracks the upstream default branch. The engine supports revision-locked overrides, but a release cannot claim stronger reproducibility than Windows while its own defaults float.

Constraints:

- Nix remains the canonical Linux package realizer. Native package managers are discovery evidence, not mutation backends.
- Endstate remains the control plane and portable schema. Nix, Home Manager, and later nix-darwin remain inspectable/ejectable implementation substrates.
- Capture is read-only and must work before Nix is installed. Backend installation remains an apply-time, consent-gated system change.
- Existing Windows modules and manifests remain valid. No existing flat module may accidentally execute on Linux merely because a path happens to exist.
- Settings capture must retain the existing secret boundary, provenance validation, backup-before-overwrite, explicit restore consent, verification, and revert guarantees.
- An existing user-owned Home Manager graph is authoritative. Endstate must not edit it or silently replace its activation.
- `endstate-gui` is a separate repository and owns no discovery logic. The CLI/envelope contract must be complete enough for the GUI to stay thin.

The lane-delegation Stage-0 audit on 2026-09-01 establishes the implementation baseline at branch commit `5a597d1fdcd552e3bbc2ae58b43d4505f7fc383f`. The branch already contains substantial immutable-input, package-catalog, discovery, schema-v3, Home Manager harvester/registry, and initial capture work, so the task ledger must distinguish accepted foundation from remaining gaps rather than dispatching greenfield duplicates. At that baseline the pinned Home Manager index contains 391 program candidates; the application matrix contains 359 rows, of which 15 are reported supported, 38 adapter-required, 306 review-required, and zero reviewed not-applicable. Those 15 are implementation candidates, not release-certified adapters: only ripgrep has an explicit schema-v3 Linux variant, the readiness predicate is not yet a required release gate, and target-content verification is incomplete.

## Goals / Non-Goals

**Goals:**

- Make a fresh Endstate install useful on an ordinary existing Linux desktop, including one with no Nix installation or Endstate state.
- Detect explicitly installed/user-facing applications and supported configuration without flooding the user with dependency packages.
- Convert only verified mappings into portable, revision-pinned Nix intents and preserve the original discovery evidence separately.
- Capture and restore a release-quality initial settings catalog through explicit platform variants.
- Meet or exceed Windows settings-module breadth for applications that actually have a Linux counterpart, with every existing Windows module classified by a generated applicability matrix rather than silently omitted.
- Use the release-pinned Home Manager module corpus as an upstream option/target schema so broad Linux support does not require hand-authoring every ordinary config path and serialization mapping.
- Provide the same opt-in scheduled drift value loop on supported Linux desktops without introducing a resident agent.
- Give the GUI one stable, product-language journey for discovery, selection, capture, apply, verification, and rollback/revert.
- Reach journey parity with Windows and exceed it where Nix provides atomic package generations and stronger reproducibility.
- Establish a Darwin-compatible module/discovery shape that the later macOS/nix-darwin change can reuse.

**Non-Goals:**

- Reproduce every installed Linux dependency or claim complete coverage of every distribution/package manager.
- Mutate apt, dnf/RPM, pacman, Flatpak, or Snap in this change.
- Infer a Nix attribute from name similarity, query an untrusted online resolver during capture, or claim an external package's version is the desired Nix version.
- Evaluate or reverse arbitrary user-owned `home.nix`, flakes, shell programs, or opaque application databases during ordinary-machine discovery. Release-time inspection of the pinned Home Manager source and pure probe configurations is explicitly in scope.
- Capture credentials, keyrings, browser profiles, histories, caches, databases, recent-item state, or machine-bound desktop state.
- Silently compose into, rewrite, or activate a user-owned Home Manager configuration.
- Ship macOS/nix-darwin support or the Linux GUI from this repository change. They receive dependent OpenSpec changes in their owning repositories.
- Support Linux schedulers other than systemd user timers in the first release; the capability remains visibly unavailable on hosts without a usable user manager.
- Claim parity by padding counts with Windows-only applications, unsafe state, aliases, or nominal modules that cannot be discovered and round-tripped. The parity floor applies to the existing Windows modules with real Linux counterparts and requires an explicit disposition for every one.

## Decisions

### 1. Product parity is an end-to-end acceptance contract

Linux is called release-ready only when the following journey passes from an ordinary, non-Endstate-managed source home:

1. Discover user-facing applications and supported settings without requiring Nix.
2. Show what is portable, what is settings-capable, and what remains unresolved.
3. Capture a selected bundle without mutating package or configuration state.
4. Apply it to a clean target through consent-gated Nix plus the declared settings realization lanes.
5. Verify both packages and settings.
6. Revert settings and roll the package generation back explicitly.
7. Complete the same journey through the shipped Linux GUI without exposing Nix knowledge in the primary flow.

This is stronger and more testable than comparing feature checkmarks or an unqualified raw count. Linux exceeds Windows at the package layer once the journey passes because the desired set commits atomically, exact release inputs are recorded, and prior generations are addressable. Linux release additionally requires the generated applicability matrix to show no unclassified or missing Linux counterpart among the existing Windows settings modules; documentation reports both applicable parity and any principled exclusions.

### 2. Inventory evidence and desired package intent are different models

Introduce an engine-owned discovery layer with two outputs:

- `InventoryItem`: what the source machine proves, including source kind, manager, native reference, display name, installed version, explicit/user-facing evidence, and non-secret evidence coordinates.
- `ResolvedPackageIntent`: the portable Endstate application ID, portable Nix attribute, immutable Nixpkgs input, resulting installable, mapping revision, and all inventory evidence that supported the resolution.

An inventory item that has no reviewed mapping remains an `UnresolvedInventoryItem`. It is returned to clients with a stable reason and is not written as an installable app. The source version remains evidence; it is not copied into `App.Version` as a request for an equivalent Nix version.

Alternative rejected: treating native package names as Nix attributes. It looks broad, but package splits, naming differences, editions, GUI/CLI variants, and Flatpak IDs make silent false matches inevitable.

### 3. Discovery uses bounded, read-only adapters and evidence reconciliation

Create `internal/discovery` with a narrow adapter interface. Each adapter executes fixed argument vectors behind a test seam and returns normalized evidence; none writes state or installs a backend.

The release adapters are:

- the Endstate-owned Nix profile;
- ordinary user Nix profiles when Nix is already available;
- explicitly installed Debian/Ubuntu packages (`apt-mark showmanual` plus `dpkg-query` evidence);
- explicitly installed RPM-family packages (the available DNF/RPM user-installed query, with a fixture-defined fallback);
- explicitly installed Arch packages (`pacman -Qe`);
- Flatpak applications and Snap applications, excluding runtimes/bases;
- XDG desktop entries from the user and system data roots;
- known executable and config-path evidence declared by the package/config catalogs.

Adapters that are unavailable report `not_available`, not failure. A selected adapter that starts but returns malformed or permission-denied data reports a source-scoped warning. Capture fails at the top level only when no selected source can produce trustworthy inventory or when writing/validating the output fails.

Evidence is merged by reviewed portable package identity, never by display name. Package-manager evidence outranks desktop/path evidence for version and native reference, while desktop/path evidence can establish user-facing relevance and configuration presence. Config-only applications may produce a settings candidate without an installable package intent.

Alternative rejected: sweeping every installed native package. Debian and RPM hosts contain hundreds or thousands of dependency packages; presenting that as a personal machine profile would make detection technically broad and practically unusable.

### 4. Package equivalence is a versioned, reviewed catalog

Add `catalog/packages/<portable-id>.jsonc`, bundled with release artifacts. Each entry owns:

- one stable portable application ID and display name;
- the Linux Nix attribute;
- reviewed discovery aliases per source (`nix`, `deb`, `rpm`, `pacman`, `flatpak`, `snap`, desktop file, executable);
- optional association with a config-module ID;
- a mapping schema/revision used in discovery provenance.

Catalog validation rejects duplicate source aliases within the same source namespace, duplicate portable IDs, missing Nix targets, and ambiguous desktop/executable aliases. CI evaluates every release mapping against the release Nixpkgs revision on supported Linux architectures. Mapping data may grow independently from the settings catalog.

Exact Nix profile elements can be captured without a native alias when their attribute identity is recoverable. Items from other managers require a reviewed catalog mapping. Unknown items remain visible as unresolved rather than disappearing or being guessed.

### 5. Release pins are immutable and recorded

Replace floating release defaults with revision-locked Nixpkgs and Home Manager inputs stored in one release-owned pin manifest. Environment overrides remain an advanced, inspectable escape hatch, but the engine resolves and records the exact revision actually used.

For a mapped external package, the portable attribute and immutable input are stored separately in discovery/provisioning data; the manifest's Linux ref may use the fully qualified installable so an apply cannot drift to a later registry target. A Nix-profile capture preserves an exact source flakeref when available. If exact provenance is unavailable, capture states that explicitly and resolves the portable attribute against the current Endstate release pin rather than pretending to recover the original expression.

Generated Home Manager flakes use the same revision-locked Nixpkgs input plus a compatible revision-locked Home Manager input. CI validates the pair and the curated option catalog before release.

Alternative rejected: relying on the user's Nix registry. It is convenient for development, but it makes two identical Endstate captures resolve differently and undermines the reason for choosing Nix.

### 6. Module schema v3 owns explicit platform variants

Add `moduleSchemaVersion: 3` with a `platforms` object keyed by `windows`, `linux`, and later `darwin`. Portable identity and common presentation stay at the top level. Each platform variant owns its matchers, instance detectors, capture/generation definitions, restore/verify definitions, secrets, path coordinates, and realization strategy.

Legacy schema-v1/v2 flat modules continue to load as Windows variants only. Schema v3 forbids mixing flat executable fields with `platforms`, so authors cannot accidentally create ambiguous authority. A module opts into Linux or Darwin explicitly.

Generation identity becomes `<moduleId>/<platform>/<configSetId>/<generationId>`. The platform variant is included in generation fingerprints, module provenance, target resolution, and diagnostics. Same-platform restore is the default. Cross-platform restoration requires an authored target mapping or migration that names both source and target variants; identical filenames alone never imply compatibility.

Platform paths use engine-owned coordinates rather than authored shell expansion: home, XDG config/data/state/cache, and platform application-data roots. The engine supplies XDG defaults when variables are absent, canonicalizes paths, and applies the existing traversal/symlink/secret gates after expansion.

Alternative rejected: duplicate `modules/linux`, `modules/windows`, and `modules/darwin` trees. That would split one portable application identity across independent curation records and make cross-platform migrations and secret boundaries drift.

### 7. Settings realization is explicit per variant

Each platform variant declares one of two realization strategies:

- `home-manager`: Endstate compiles typed settings or captured file placements into its generated, inspectable Home Manager module and gains a Home Manager generation for rollback.
- `endstate-restore`: Endstate uses the existing backup-before-overwrite, explicit-consent, validation, journal, and revert path for opaque or application-specific files that Home Manager cannot safely model.

The strategy is catalog authority, not a runtime guess. A bundle records it and current trusted catalog rules decide whether it remains valid at restore time.

When no Home Manager activation exists, Endstate may own the generated Home Manager configuration. When a live Home Manager activation exists without matching Endstate provisioning history, it is treated as user-owned. Endstate still captures supported settings, but apply must either emit an inspectable importable module for the owner or use an explicitly declared safe `endstate-restore` fallback after consent. It never edits or replaces the external graph automatically.

This preserves Home Manager as a first-class settings realizer without turning Endstate into a hostile wrapper around users who already know Nix.

### 8. A pinned Home Manager adapter registry supplies breadth; reviewed codecs supply inversion

Home Manager is an unusually rich upstream schema but not a generic discovery engine. Its release output exposes machine-readable option metadata, its module declarations identify program ownership, and a pure probe evaluation can reveal the `home.file` targets added by enabling a program. Endstate SHALL harvest those inputs at release time from the same immutable Home Manager revision used for realization and commit/bundle a deterministic adapter registry. Runtime discovery reads only that frozen registry and the live user environment; it does not require Nix, fetch Home Manager, parse arbitrary Nix, or evaluate the user's configuration.

The harvester records the program/module identity, option metadata, declaration/source hash, default package evidence, evaluated managed targets relative to engine-owned home/XDG coordinates, and a reviewed capture disposition. Each settings surface receives exactly one disposition:

- `typed-roundtrip`: a supported application format such as JSON, YAML, TOML, INI, or a deliberately implemented domain codec can be decoded from live state and re-emitted through the corresponding Home Manager settings option with round-trip proof;
- `file-roundtrip`: the target is safe portable configuration but the Home Manager generator is not reliably invertible, so Endstate captures the bounded live file and restores it through inspectable Home Manager `home.file` placement;
- `curated-codec`: the module combines defaults, fragments, scripts, migrations, or multiple targets and needs an explicit reviewed capture mapping;
- `excluded`: the state is absent on Linux, credential-bearing, historical, cached, database-backed, machine-bound, or otherwise unsafe, with a stable reason that is visible in the coverage report.

Generated metadata is candidate evidence, not capture or execution authority. A non-excluded registry entry becomes an advertised adapter only when an explicit schema-v3 Linux variant binds the same portable identity, package/module association, target set, realization strategy, actions, and exclusions. Registry validation rejects a target or source hash change until its disposition is re-reviewed through a bounded deterministic diff. Every supported disposition must pass live detection plus capture/apply/target-content-verify/revert tests. A Home Manager generation-number match is ownership evidence, not settings verification. This avoids both hand-copying hundreds of Home Manager declarations and pretending that an arbitrary Nix function, a bounded file size, or a legacy Windows module supplies an automatic safe inverse.

The release builds an applicability matrix joining every existing Endstate Windows module with the package catalog, Home Manager programs, Linux desktop/executable evidence, and persistent explicit review overrides. Every Windows module that has a real Linux counterpart must resolve to a supported Linux variant before parity can be advertised; a platform/safety exclusion is allowed only when it explains why equivalent portability is not meaningful or defensible. Required release validation invokes the readiness predicate directly, so an optional report command cannot pass with blocking rows. Safely derivable Home Manager programs with no Windows module are additive Linux coverage and can make Linux broader than Windows only after receiving the same explicit schema-v3 authority and lifecycle proof.

The named release-blocking depth corpus remains Git, Bash, Zsh, SSH configuration without keys, tmux, direnv, Starship, fzf, zoxide, bat, eza, ripgrep, fd, Neovim, Helix, WezTerm, Kitty, Alacritty, GitHub CLI without authentication, lazygit, Jujutsu, Atuin without account/session material, and Yazi. It proves every capture disposition and the complete live journey early; it is not a launch ceiling. Generic byte preservation is acceptable only for a reviewed data-only target whose contents cannot encode commands, secrets, accounts, remote-service state, or machine-bound paths. Command-bearing or secret-capable formats require a semantic codec, narrower value adapter, visible capture-only treatment, or exclusion. A module may be capture-only if safe automated restore is not yet justified, but that limitation is visible in discovery and does not count as full parity with a restorable Windows counterpart.

GNOME and KDE receive a separate value-scoped tier inside the same platform-module contract, after a shared durable typed-value transaction foundation exists. That foundation owns stable target identity, prior value-versus-absence snapshots, atomic intent/journal persistence, commit verification, partial-failure classification, interrupted-operation recovery, and explicit revert:

- GNOME captures an allowlist of schema/key values through an engine-owned GSettings boundary that distinguishes a user override from an inherited default. Apply backs up the previous user value or absence, writes only the named value, verifies it, and revert restores the value or resets the override.
- KDE captures an allowlist of KConfig file/group/key values through version-aware engine operations. It distinguishes absence from an empty value, merges only the named keys while preserving co-resident values, backs up prior values/absence, verifies them, and reverts them.

Whole dconf databases, whole KDE config directories, accounts, keyrings, histories, recent files, device topology, and session/window state are forbidden. This mirrors the safety lesson from Windows registry value capture: value-level ownership is the only defensible OS-settings tier. Both desktop capabilities remain dark until their exact initial allowlists, value-type encodings, supported release ranges, and current real-session acceptance gates pass.

### 9. Scheduled drift uses a systemd user timer and the shared command contract

First reconcile the active Windows-first `scheduled-drift-check` change and public CLI contract into one platform-neutral command/result model covering `schedule enable|disable|status|run|discard-upload`. Replace the Windows-shaped pre-quoted registrar with a structured scheduler interface that owns availability, enable, disable, inspect, and native argument rendering. Preserve all Windows behavior through regression tests before adding Linux. On Linux, `enable` writes inspectable user units under the resolved XDG systemd user directory and enables an `endstate-drift-check.timer`; the paired service invokes the short-lived engine `schedule run` and exits. The timer uses `Persistent=true` so a missed run is caught after the user manager resumes. `disable` disables the timer and removes only Endstate-owned generated units; `status` reconciles saved intent with live availability, unit ownership/content integrity, enabled/active state, and next-trigger state.

The Linux path never requires root and never creates a system unit. A working `systemctl --user` manager is required for registration capability; when unavailable, registration reports unsupported with remediation and the rest of Linux remains ready. Manual `schedule run` and durable Cloud replay/discard remain scheduler-independent. The service receives the same explicit engine/root/manifest coordinates as the Windows task so GUI and scheduled invocations share state. Generated units carry an ownership marker/version and content hash; explicit re-enable refuses silent overwrite when a recognizably owned unit was user-modified. Registration is recoverable across partial unit-write, reload, enable/start, and config-persistence failures. No long-running Endstate process is introduced.

Alternative rejected: cron fallback in the first release. Cron environment, missed-run behavior, per-desktop availability, and uninstall ownership vary enough that a silent fallback would be harder to inspect and remove safely. It can be added later as an explicit scheduler backend.

### 10. The CLI exposes one additive discovery contract

Linux `capture` runs discovery by default. Existing `appsIncluded`, config-module, artifact, and summary fields remain for GUI compatibility. Add a `discovery` object containing:

- source status/count/warnings;
- resolved package intents and their evidence;
- settings candidates and supported actions;
- unresolved user-facing inventory with stable reason codes;
- counts that distinguish discovered, resolved, selected, unresolved, and ignored dependency/runtime items.

Structured events expose source progress and resolved/unresolved item status without streaming raw package-manager output. Primary messages use Endstate terms such as “application source unavailable” or “could not map this app”; backend names, commands, and raw output stay in inspectable details.

Capabilities advertise discovery schema/version, available source adapters on this host, platform-module support, and whether the release pins are immutable. The GUI derives its controls from capabilities and the capture result; it does not inspect package managers, Nix, module files, or the filesystem itself.

### 11. CI and release claims are gates, not documentation optimism

Required verification includes:

- hermetic fixtures for every adapter, reconciliation rule, catalog ambiguity, and hostile/malformed output;
- catalog validation and release-pin evaluation on Linux amd64 and arm64 where the package exists;
- distro discovery jobs for Ubuntu/Debian, Fedora/RPM, and Arch fixtures or containers;
- a capture test starting with no Endstate profile and no callable Nix binary;
- a required Ubuntu real-Nix/Home Manager journey that captures known native/config state, applies to isolated clean state, verifies, reverts settings, and rolls the Nix generation back;
- GNOME/KDE operation tests plus real desktop-session acceptance on at least one current release of each before claiming that desktop tier;
- Linux release-binary bootstrap and artifact smoke.
- a Linux user-timer lifecycle smoke covering enable, status, forced run, persisted result, disable, and generated-unit cleanup without touching system scope.

The existing real-Nix job cannot remain `continue-on-error` for the release-blocking Linux leg. Compatibility docs distinguish “engine substrate available” from “ordinary-machine product journey verified” until all gates pass. The Linux announcement additionally depends on the separate GUI packaging/E2E gate.

### 12. macOS follows the same contracts, in its own change

Schema v3 reserves `darwin`; the package catalog supports Darwin targets/aliases; discovery evidence is source-qualified; settings realization already distinguishes Home Manager, direct safe restore, and external ownership; and the schedule command has a scheduler interface ready for launchd. A later `add-macos-product-parity` change supplies Homebrew and Mac App Store discovery, nix-darwin/value-scoped macOS user settings, launchd registration, the Darwin curated catalog, GUI packaging, signing/updater validation, and a real-Mac acceptance pass.

nix-darwin is not used as a generic machine-discovery system and Endstate will not rewrite an existing nix-darwin graph. It is a system/user-settings realizer beneath the same Endstate capture and ownership model.

### 13. Discovery totals separate raw observations from unique user-facing candidates

Per-source `sourceDiscovered` counts describe raw normalized observations and may overlap. Top-level counts describe unique candidates after evidence reconciliation. A candidate with a resolved package intent belongs to `resolved` even when it also has settings; a config-only candidate belongs to `settingsOnly`; every other user-facing candidate belongs to `unresolved`. The existing `settings` count remains backward compatible and counts every unique candidate with at least one supported settings action, so it may overlap `resolved`; `settingsOnly` is its no-package subset. Therefore `discovered = resolved + settingsOnly + unresolved`, `settingsOnly <= settings <= discovered`, ignored dependency/runtime observations stay outside the denominator, and `selected <= discovered`. Selection is explicit and never aliases the resolved count.

This gives the CLI, GUI, fixtures, and release evidence one independently checkable accounting model without hiding source overlap or double-counting apps with both package and settings state.

Discovery retains complete public inventories when a caller selects a subset.
A resolved package uses a typed package identity with its portable ID. Only an
explicit reviewed settings package ID joins that package identity. Other
settings use a separate typed settings identity, even when their ID text
matches a package ID. Unmapped inventory retains its source-qualified identity.
Settings row IDs remain the keys for private file plans. Multiple settings rows
can describe one candidate.
Presence-only settings remain visible but grant no action; without a resolved
package, their candidate belongs to the unresolved partition.

The existing command selection validator owns CLI namespaces and unknown-ID
errors. Capture selects execution plans through exact settings, module, or
package IDs. Public inventory and raw source counts remain complete. The
selected count uses the union of selected package and supported settings
candidates. Sanitization omits settings execution.

An update preserves existing manifest apps while it scopes live config
collection to this run's selected apps and modules. Retained unselected apps
do not authorize additional collection. Historical Home Manager declarations
remain subject to selection, sanitization, and share boundaries.

Settings-only success requires useful payload from actual collection. The
bundle collector checks that result before publication, including generation,
legacy, and Home Manager files. Planned optional files or blocked ownership
alone do not establish a successful capture. Sanitized capture requires useful
selected package state because it deliberately omits settings.

Repeated equivalent captures retain honest timestamps. Acceptance compares
stable serialized discovery, payload bytes, and bundle structure. It excludes
only documented capture-time and archive-entry-time fields; all other metadata
remains part of the comparison. No replay or frozen observation time is implied.

### 14. Linux artifacts are complete runtime bundles; GUI launches with AppImage plus deb

The ordinary-machine contract cannot depend on a source checkout or ambient `ENDSTATE_ROOT`. Every shipped Linux engine artifact carries a version-matched binary, modules, payload, package catalog, frozen Home Manager registry, and schema identity. Unix bootstrap installs or resolves that complete asset set, and release smoke proves lookup plus no-Nix capture from shipped amd64 and arm64 engine artifacts.

The dependent GUI change initially produces a portable AppImage and an installable deb, because Ubuntu is the required fresh-machine acceptance host. RPM packaging is a follow-on unless the GUI change explicitly adds a Fedora installed-package claim. The GUI bundles the same engine-owned runtime assets and contains no package-manager, Nix, module-catalog, or filesystem discovery logic. The GUI launch architecture set is declared by that change and may be narrower than the engine set, but documentation and updater metadata advertise only architectures with installed-package evidence.

### 15. Delegation follows a gated DAG, not the numeric backlog alone

The task ledger records the audited existing baseline and then executes through bounded packets: plan/overlap reconciliation; platform provenance, immutable catalog/provisioning, and discovery completion; capture-envelope integration; registry/module authority and codec safety; isolated adapter cohorts plus serialized registry integration; durable desktop-value foundations followed by GNOME/KDE; shared scheduler extraction followed by systemd-user support; release/real-host gates; dependent GUI delivery; and the macOS follow-on.

Every implementation packet names red-first tests, exact scoped gates, exclusive/shared-file ownership, unique writable cache/state roots, and stop conditions. Each receives a fresh author-independent reviewer over its actual diff. Adapter cohorts do not concurrently edit the generated Home Manager registry or parity matrix; one serialized integration packet owns those shared artifacts. A successful full Go corpus runs once at each completed engine tranche boundary, not redundantly in every lane.

### 16. Overlapping active changes are serialized by surface

Four other active changes touch surfaces this change also edits. Their state was verified on 2026-09-01 against branch commit `5a597d1fdcd552e3bbc2ae58b43d4505f7fc383f`, which contains `origin/main` `89d81ddc5382d4f29a8c42856b34e768226942f6`:

| Change | Verified state | Shared surface | Rules |
|---|---|---|---|
| `scheduled-drift-check` | Implemented on main: `internal/schedule`, `internal/commands/schedule*.go`, the `features.schedule` capability, the CLI contract section "Command: `schedule`", and replay/discard from #218. Its ledger is unreconciled (no task checked) and its delta spec is not synced into `openspec/specs`. | Section 11 | R3 |
| `catalog-discovery-hardening` | Implemented on main: `walkUpFor`, `installCatalog`, and the `module_catalog_unavailable` warning. Ledger complete except follow-ups; delta spec not synced. | 3.6, 12.6 | R5 |
| `home-catalog-data-driven` | Implemented on main; this branch already extends `internal/realizer/nix/home_catalog.go`. Tasks 4.4 (real-Nix activation smoke) and 4.5 (validation) remain open; delta spec not synced. | 7.2, section 9 | R4 |
| `verified-module-matrix` | PR #205 and #214 merged; hosted-live work remains separately owned; ledger open; delta spec not synced. It also adds requirements to `config-generation-modules`. | Sections 5, 9, 12 | R1, R2, R6 |

Task 1.5 syncs and archives the three implemented changes in one cohesive documentation batch based on current `origin/main`. Shared implementation work follows that batch in dependency order. Until each archive lands, the rules below bind every packet emitted from this change:

- **R1 Validation sidecars.** Every `modules/<tier>/<id>/validation.jsonc` binds `moduleRevision` to the bytes of its `module.jsonc`. A packet that edits any `module.jsonc` SHALL refresh sidecars with `cd go-engine && go run ./cmd/endstate-validation sync-revisions --repo <absolute repo root> --write` and SHALL run `cd go-engine && go test ./internal/validationmatrix` (with the packet's `GOCACHE`) in its gate. A packet that adds a module directory SHALL add its sidecar and SHALL report the resulting drift against the `verified-module-matrix` denominators (367 records and 370 logical rows at main commit `d76d3e5`; the Linux application matrix remains a separate 359-row inventory) for review. Author lanes do not edit `.github/validation/synthetic-known-failures.json`. The serialized integration owner may add reviewed inventory entries or remove debt only after real passing evidence proves resolution. New debt and failure-fingerprint transitions still require protected-main authority; integration does not change proof semantics.
- **R2 Matrix harness and workflows.** `.github/workflows/go-ci.yml`, any workflow that change adds such as `.github/workflows/hosted-live.yml`, `go-engine/cmd/endstate-validation`, and `go-engine/internal/validationharness`, `validationmatrix`, `validationmode`, and `validationci` are `verified-module-matrix` surfaces. A packet SHALL name each such file it edits, SHALL limit edits to what schema-v3 or Linux projection requires, and SHALL NOT change shard policy, runner policy, quarantine semantics, or proof semantics.
- **R3 Scheduler.** Section 11 is blocked until `scheduled-drift-check` has its ledger reconciled against the shipped implementation and is archived through task 1.5 so that `scheduled-drift-check` exists in `openspec/specs`. Task 11.1 then authors this change's `scheduled-drift-check` MODIFIED delta for the shared scheduler interface and registrar replacement and modifies the public CLI contract; `linux-scheduled-drift-check` remains an added capability. No packet edits `internal/schedule` or `internal/commands/schedule*.go` before that.
- **R4 Home Manager catalog.** `internal/realizer/nix/home_catalog.go` and its `curatedTable` pass to sections 7.2 and 9 of this change once `home-catalog-data-driven` is synced and archived through task 1.5; the edit already on this branch (commit `969f20e`) is accepted baseline and is not re-serialized. That change's task 4.5 (OpenSpec validation) is satisfiable today (100 passed, 0 failed on 2026-09-01) and is ticked by the archive change. Its task 4.4, one real-Nix `home-manager switch` activation per `curatedTable` field kind (`kindNone`, `kindString`, `kindStringMap`, `kindAnyMap`, `kindStringSlice`), is carried explicitly by task 9.15 of this change.
- **R5 Unix bootstrap and resolution order.** Task 12.6 owns Linux catalog installation: `internal/commands/bootstrap_unix.go` plus the shared `installCatalog` and `catalogDirs` in `internal/commands/bootstrap.go`, which `bootstrap_windows.go` also calls. A packet that changes the shared installer SHALL name `bootstrap.go` and SHALL keep the existing Windows bootstrap tests green. Task 12.6 absorbs `catalog-discovery-hardening` follow-up 5.2 and dispatches only after that change is archived through task 1.5. Task 3.6 builds on the four-step root resolution order that change defined and SHALL NOT reorder it.
- **R6 `config-generation-modules`.** This change and `verified-module-matrix` each ADD distinct requirements to that capability: "Platform variants qualify schema-v3 generation identity" and "Legacy module generations are Windows-only" here, "Studio One Generation Validates Its Core Settings File" there. Whichever change archives second SHALL re-validate its delta against the synced main spec before archiving.

### 17. Recovery retains current main and its catalog boundaries

Recovery integrates main commit `d76d3e522f4141ba9750d84f00ef90d70ac20c68` while preserving the approved Linux implementation and plan. Main retired the old `docs/ai` hierarchy; `CLAUDE.md` and the current operator instructions govern new packets. Protected-path assignments and the existing release holds still apply.

Main loads application and desktop catalog tiers together. Linux application parity filters the catalog by its authored application tier. Desktop modules retain their separate value-scoped acceptance gates. Validation still covers all tiers; its 367-module, 370-scenario inventory must not be confused with the 359 application rows.

OpenSpec uses the repository-selected version `1.14.0`, pinned exactly so a fresh install does not change validation policy. Delta scenarios retain the current spec's scenario identities while applying the approved Linux refinements. Darwin empty-profile capture remains unchanged.

### 18. Native Nix derivations freeze provisioning before mutation

Nix resolves each requested installable through its own parser and evaluator. The engine retains the requested reference, effective root provenance, and the native lock graph. Native dry-run build JSON identifies the exact derivation and selected outputs. A native priority evaluation must identify that same derivation. Installed versions remain observations.

One input-free metadata flake exposes the frozen derivations through native derivation imports and preserves each package's priority. Its immutable source supplies every reference in one atomic profile addition. The engine records the source/attribute pair and observed output paths alongside the original application identity. Current inventory verifies that pair and restores the original identity before plan, verify, prune, discovery, or capture. Native removal keys remain unchanged. Capture never exports the metadata flake or labels floating provenance reproducible.

Generated Home Manager work resolves and builds its frozen activation before package mutation. Native roots retain prepared derivations and activation outputs through finalization. Activation uses the existing store-script mechanism with the native backup environment. External ownership remains explicit. New ownership evidence compares the active output with the recorded output, rather than trusting a generation number alone.

New provisioning records retain configuration identity and effective inputs without storing settings payloads. A settings-generated record may reference the declared source manifest and its settings digest. Capture recovers declared settings only when that source still matches the recorded digest. Otherwise it reports the unavailable declaration and preserves independent live-settings discovery. Legacy embedded settings remain readable. Generated machine-local flakes never become portable capture input.

Finalization records every committed backend generation even when a later stage fails. Persistence failure remains visible as committed state whose provenance could not be saved. Rollback uses the recorded native snapshot or a retained frozen derivation; it never re-evaluates the latest generated configuration.

This extends the existing Nix backend and provisioning records. A copied source-flake system would change source metadata and duplicate native locking. Raw derivation addition would lose package priority and identity. The small metadata flake is required to preserve those existing package semantics.

### 19. Platform provenance extends the existing generation pipeline

Schema-v3 generation identity consists of the module, platform, config set, and local generation ID. Local generation IDs and migration endpoints stay unchanged. Captures persist `sourcePlatform`; one formatter supplies qualified history identities without storing a second identity string.

The parser hashes a canonical envelope containing module ID, platform, config-set ID, generation ID, and the selected variant definition. It excludes computed fingerprints, historical acceptance lists, and inbound mappings from decision 20. JSON numbers retain their exact values. Legacy fingerprints remain unchanged. Changes to another platform do not invalidate the selected variant. Changes within that variant require explicit historical acceptance.

Executable projections own their mutable declarations. They retain the full authored snapshot and revision for inspection. Generation history reads its legacy schema and writes schema version 2 with an optional platform field. Legacy entries retain their original identities and fingerprints.

Flat schema-v3 variants with capture or restore work use the same generation pipeline. Projection supplies the fixed shorthand identity `default/g1` and one existing path detector at `${home}`. That detector identifies a user settings location; it proves neither installation nor configuration presence. Projection moves capture and restore declarations into that generation and clears their top-level copies. It preserves matching, verification, secrets, realization, and presentation. Only the fixed `./payload/` restore-source prefix is removed; capture destinations remain unchanged. Actual schema-v1/v2 declarations retain their existing behavior.

Projected schema-v3 detector globs, capture sources, and restore targets accept engine-owned coordinates through the shared validator and resolver. Portable destinations and staging sources retain their stricter rules. Paths remain declarative until host resolution; catalog validation does not receive expanded absolute paths.

Schema-v3 capture records retain the authored schema and require a supported source platform. Snapshot verification compares recorded and authored schema for both schema 2 and schema 3. For schema 3, it also checks the recorded module, source variant, config set, generation, and exact recomputed fingerprint. It reuses the same projection for shorthand variants and rejects synthetic schema-v3 stamping. The snapshot supplies source evidence only; the current trusted catalog remains execution authority.

Missing structural platform evidence fails manifest loading under the existing strict contract. Semantic snapshot-link failures use the existing per-capture invalid state, so independent valid work can continue. Resolution events, reports, and strict validation readers retain both platform fields and the stable skip reason.

Task 5.4 includes the same-platform guard from task 5.5. The resolver checks source and target platforms before generation or fingerprint acceptance. A different platform returns `platform_mapping_missing`, including when an explicit target instance was selected. Existing legacy sources remain Windows-scoped. Full authored cross-platform mappings remain separate task 5.5 work.

The proof captures a Linux generation and restores it through its compatible Linux variant. The same bundle offered to a Windows variant must leave target bytes unchanged and create no transaction, even if that variant accepts the source fingerprint. The existing resolver, staging engine, and journal provide this behavior; no second restore engine is required.

The matrix represents normalized flat schema-v3 shorthand with the existing `capture-contract` scenario and only `catalog` plus `engine-contract` proof. The adapter must establish the original absence of authored `config` from the selected variant, not from shorthand IDs alone. It preserves exact payload, source snapshot, platform, fingerprint, content, and optional-absence checks. An independent workflow test proves actual capture and restore bytes. Generation and migration scenarios retain their non-zero production-validation requirements; a shorthand with no declared validation receives no config-roundtrip proof credit.

### 20. Target variants own one-step platform mappings

Task 5.5 reuses the existing migration operations, validators, staging engine,
and configuration transactions. It adds no cross-platform route search.

A target variant may declare `mappings`. Each mapping has a unique `id`, typed
`from` and `to` endpoints, `acceptsSourceFingerprints`, `operations`, and
`validate`. Each endpoint names `platform`, `configSetId`, and `generationId`.
The containing module supplies module identity. The target endpoint must name
the containing variant; the source must name another platform.

Accepted source fingerprints must be nonempty, unique SHA-256 values. An
explicit empty `operations` array declares an unchanged payload representation.
Other operations use the existing migration operation types. Nonempty mapping
validation is mandatory. Each validation path must resolve through a target
restore declaration, so the engine can check staging and committed state.

The source snapshot and payload remain integrity evidence. The current target
catalog supplies mapping authority. A historical source generation need not
remain executable in the current catalog. Repository history checks bind each
accepted fingerprint to its declared source identity.

Positive mappings accept genuine schema-v3 sources and implicit Windows
schema-v2 sources. Existing snapshot verification computes a stricter internal
link result for schema 2: exact module, set, generation, and recomputed
fingerprint. Cross-platform acceptance requires that result. Same-platform
legacy reads keep their current behavior. Repository checks use qualified
history for schema 3 and implicit Windows history for schema 2. Runtime does
not reload history or treat it as execution authority.

The planner matches the source endpoint and fingerprint, then selects the
current target generation through existing version evidence. Exactly one
mapping must end at that generation. Overlapping source acceptance must identify
one target set. Duplicate bridges are invalid catalog data. Missing acceptance
keeps `platform_mapping_missing`; target ambiguity keeps existing generation
diagnostics. An explicit instance selection cannot bypass mapping authority.

Each mapping supports one source set and one target set, including explicit
set renaming. Existing `configSetId` remains source identity. Additive
`targetConfigSetId` identifies the target and defaults to the source for old
plans. Collision checks use the target set. Split, merge, cross-module, and
multi-hop mappings remain outside this contract.

The immutable plan pins the mapping, target set, target generation, module
revision, and target path policies. The staging request carries a separate
mapping step. Equal local generation IDs and empty operations remain valid
there; local migration-edge rules remain unchanged. A bridge ends at the
selected generation without an additional target-side migration route.

Mapping validations run after staging. Materialization appends them to target
generation validations for committed checks and journal persistence. Each
mapping check identifies exactly one non-excluded path that the restore
actually transfers. A valid pre-existing target cannot substitute for that
transfer.

Target restore declarations control destinations and copy exclusions. Target secret
declarations and applicable target capture exclusions also govern mapped
members before backups or writes. Capture exclusions retain their declared
source-root scope. Reuse existing path and glob matchers; never treat those
checks as semantic proof that content contains no credentials. A forbidden
mapped member fails the set before mutation, with the existing safety reason.
The user can retry after correcting the payload or target declaration.

Materialization checks target policy after resolving source and destination
pairs, before preparing actions. It inspects only transferred members. A copy
member maps to its destination plus its relative suffix; merge and append
operations use their exact destination. Target capture sources resolve through
the same instance and host boundary. For each applicable source, exclusions
first check its basename, then any directory-relative member. A sibling source
stays outside that scope. Existing restore exclusions determine transferred
members.

The shared secret matcher accepts already-resolved paths and explicit platform
case rules for this boundary. Existing capture wrappers retain their behavior.
Linux matching remains case-sensitive, and malformed patterns produce errors.
The engine never expands resolved paths again or scans unrelated stage files.

The new matching boundary uses `github.com/bmatcuk/doublestar/v4` at version
`v4.9.1`. The standard matcher lacks recursive glob semantics. The existing
secret matcher misses combined forms such as `**/token*.json`. One shared
entry point uses the maintained parser and preserves directory-secret and
basename policy. Existing capture wrappers retain their compatibility behavior;
their mixed-wildcard limitation remains explicit debt. The added maintenance
cost is one pure Go dependency with no transitive module requirements.

The mapped restore proof includes a forbidden `nested/token1.json` member
matched by a recursive wildcard plus a filename wildcard. It checks the
integration boundary, not the library's pattern corpus.

Inbound mappings are compatibility authority, separate from generation
meaning. Exclude `mappings` from generation fingerprints alongside acceptance
metadata, while retaining the entire mapping in canonical snapshots and module
revisions. This prevents compatibility edits from changing capture meaning or
creating reciprocal fingerprint dependencies.

Reports, events, and journals retain the mapping identity, both platforms, and
the target set. Journal validation distinguishes a platform bridge from a
local migration. Old journals keep their existing interpretation and remain
readable. The proof captures a real Linux fixture, restores through an authored
target mapping, checks contents, and reverts. Hostile cases cover missing
acceptance, equal local IDs, ambiguous mappings, transformed forbidden members,
and failed staging or target validation.

### 21. Prove representative state lifecycles before expanding catalogue breadth

The product manages desired machine state. An observed value is evidence, not
automatic permission to adopt or maintain it. The user selects the desired
state and the surfaces that Endstate owns. Keep resource identity, ownership,
comparison, mutation, verification, and recovery explicit in each existing lane.

Begin with one existing package and its declared file configuration. Reuse the
package realizer, platform module, capture bundle, planner, verifier, and durable
restore journal. Prefer the existing Linux ripgrep variant for the first
workflow. A controlled fixture proves only its declared content; it does not
authorize arbitrary command-bearing configuration or establish codec safety.
Apply the existing reviewed content-policy requirements before advertising it.

The outcome check observes source state, selects desired state, previews changes,
applies to isolated target state, and verifies the package and configuration
contents. It then changes a managed value, observes drift, reconciles the value,
verifies the result, and exercises the existing resource-specific undo. Include
an unrelated value and prove that the workflow preserves it. Reuse existing
proof where its artifact, runtime, and conditions still match.

Extend the same journey with one allowlisted desktop preference and the existing
user-scope drift schedule. Desktop acceptance distinguishes an explicit override
from an inherited default and restores prior absence on revert. Schedule
acceptance preserves foreign units and modified user files, detects declaration
drift, and exercises explicit reassertion and ownership-checked disable.
Use the existing declarations and schedule intent store; this milestone adds
no combined profile format, generic service manager, or new CLI command.

These surfaces share a lifecycle, not necessarily a transaction or inverse.
Package rollback, configuration revert, desktop reset, and schedule disable
retain their own contracts. Report partial failure and recover each owned
mutation; do not promise atomic rollback across all resources. Extract shared
interfaces only after the working adapters expose the same invariant.

Prioritize these proofs and the implementation that they require before routine
cohort expansion. This orders delivery; it does not reduce the applicable
catalogue parity requirement. Native desktop and systemd evidence remains
mandatory before their promotion. A hermetic adapter check proves its boundary,
not a real desktop session. Missing native evidence holds the corresponding
capability and leaves independent accepted work dispatchable.

Dependencies name consumed results. Same-platform capture and its accounting
do not consume cross-platform mapping acceptance. They may be prepared and
tested against a pinned candidate while the separate mapping review remains
held. Cross-platform execution and delivery of unreviewed changes still wait
for their exact review. This rule grants no permission to retry or bypass a
blocked review tool, waive a release gate, or overwrite another lane's work.

### 22. Bind reviewed adapters to the generation that owns their lifecycle

Reuse the existing module catalogue, frozen registry, settings codecs, and
configuration transaction. Registry metadata describes possible adapters;
the Linux variant supplies executable authority. No new adapter framework or
second authored target/action list is required.

A Linux `PlatformVariant` may declare `homeManagerAdapter` with `id`,
`reviewFingerprint`, `configSetId`, and `generationId`. Preserve that binding
in executable projection and include it in existing variant fingerprints.
The binding identifies one registry entry and one existing generation,
including `default/g1` for the established flat shorthand.

One pure authority join consumes the validated registry, authored module
catalogue, and package catalogue. Discovery, capture planning, and the parity
matrix consume its result. The join requires explicit schema-v3 Linux
authority, exact reviewed fingerprint, matching module/package association,
and agreement between registry targets, generation capture sources, and
restore targets. It checks secret exclusions and the implemented content
policy before granting actions. Mechanical codec availability alone is
insufficient. A bounded byte-preserving codec does not establish semantic safety.
Keep structural binding distinct from executable capability in that one result.
A structurally valid binding may have no actions while its strategy lacks
semantic policy, target-content verification, or recovery. Existing generic
byte-copy and Git denylist codecs do not supply that missing proof.
Unbound or policy-blocked observation uses metadata inspection only. It must
not open, hash, transform, or collect payload bytes to establish presence.

Derive actions from the bound generation, declared realization strategy,
implemented content policy, verifier, and recovery path. Do not add an authored
action list that can drift from those mechanisms. A missing binding leaves
candidate evidence visible without actions or private collection plans.
A contradictory binding produces a bounded entry-specific diagnostic and
prevents that adapter's execution. Independent valid adapters remain usable;
release readiness requires every advertised adapter's authority to agree.
This control prevents unsupported file capture and misleading support claims.
A false rejection withholds one adapter while preserving its evidence and
independent work; it must not abort unrelated discovery.

Keep the first ripgrep variant on `endstate-restore`. Reconcile its reviewed
targets to `${home}/.ripgreprc`, `${xdg.config}/ripgrep/config`, and
`${xdg.config}/ripgrep/ripgreprc`. Use a versioned semantic codec for a reviewed
portable subset of ripgrep arguments. Upstream configuration treats each
trimmed non-comment line as one argument; it does not use shell parsing.
The upstream [configuration reader](https://github.com/BurntSushi/ripgrep/blob/master/crates/core/flags/config.rs)
supplies the grammar; a small argument scanner and closed reviewed flag schema
avoid duplicating ripgrep's full command-line parser.
Unknown, positional, command-capable, or path-bearing arguments leave that file
unavailable for automatic capture/restore. Do not silently drop an argument
and claim equivalent settings. Preserve the order of accepted arguments.
Canonical output may omit comments. Application behavior proof supplies
`RIPGREP_CONFIG_PATH`; this change does not manage the user's shell environment.

The trusted generation declares `settings-codec` validations with a codec and
optional-file handling. Each validation maps its payload path through one
existing copy declaration to the reviewed target. Capture rejects unsupported
content during canonicalizing collection. Restore reuses staged validation and
journaled committed validation. A missing optional source creates no validation
of a pre-existing host file; every transferred file requires validation.

The harvester remains the producer of pinned metadata and observed probe
targets. Curated runtime aliases remain reviewed registry data. Refresh does
not manufacture module bindings; changed metadata invalidates the old binding.

Binding and content-policy changes alter Linux generation fingerprints.
Introduce the reviewed ripgrep policy as explicit Linux generation `g2` and
retain the earlier `g1` history. Do not automatically accept old byte-copy
fingerprints under the new policy. Old captures remain readable; restore needs
a reviewed migration or semantic revalidation before they become executable.
Do not weaken repository history validation to reuse the old generation ID.

Collection consumes the selected observed target/hash set. Selecting a module
does not authorize a second unconstrained read of its other sources.
Carry that fence as private execution data while retaining the complete pinned
generation declaration and fingerprint. Do not filter the declaration to
implement selection or change the meaning of its source provenance.
`endstate-restore` captures use existing `ConfigCaptures`, staged validation,
journals, content verification, and revert. They must not also populate
`homeManager.settings.files`. Home Manager placement uses its declared strategy
and must not execute the native restore lane for the same target.

For generated Home Manager files, record bounded target/hash expectations
beside activation identity from the exact staged bytes used for activation.
Verification proves activation ownership and expected target contents.
Rollback verifies the prior activation's recorded expectations. Legacy history
remains readable, but absent content evidence is visibly unverified. Typed
settings retain their separate semantic comparison requirement.

Implement the shared authority join first, then the reviewed native ripgrep
lifecycle, then Home Manager content expectations and remaining adapter bindings.
The first join does not complete task 8.4 for the whole registry. Reuse current
file drift verification; retain native runtime, mapping review, full catalogue,
GUI, and delivery obligations. Existing Windows contracts remain unchanged.

### 23. Extend the existing transaction with one concrete desktop value

Use the existing configuration preparation, snapshots, mutation lease, journal,
comparison, recovery, and revert for the first GNOME preference. Add one
`desktop-value` action and its injected reader/mutator boundary. Keep filesystem
and registry behavior intact. Do not add a second transaction framework or
implement unused KDE fields before its adapter needs them.

Begin with `org.gnome.desktop.interface/clock-show-date`, a boolean at
`/org/gnome/desktop/interface/clock-show-date`. A canonical bounded JSON document
records its type, explicit user-value presence, and the value when present.
Absent user state contains no value. Keep the effective default separate;
capturing inheritance means retaining inheritance on the target machine.
Reject malformed, contradictory, unsupported, and trailing payload data.
An unavailable read never produces an absence document.

The trusted schema-v3 Linux generation declares the desktop target, expected
type, capture payload path, and `endstate-restore` strategy. The payload supplies
desired state, never target authority. Reuse generation hashes, snapshots,
fingerprints, staging, restore consent, and standalone content verification.
Add a genuine desktop detector; neither a fake package nor a dconf file proves
that the required desktop boundary is usable.

GNOME target identity uses the resolved backing path and key, preserving case.
The schema remains authority metadata; two schemas that own the same backing
key conflict. Bind private transaction snapshots to the resolved user backend
and profile namespace. Keep ephemeral process and bus identifiers outside that
stable scope. The first native implementation supports proven fallback
`user-db:user`. Establish the actual dconf backend and prove that every profile
override candidate for the supported runtime is absent. Bind the user ID and
GLib-resolved configuration directory's `dconf/user` namespace. Custom or service
profiles remain unavailable until their resolution has a supported implementation;
do not infer fallback from environment variables alone or add a profile parser.
Changed scope or an unrelated third value prevents blind recovery
over another owner. The existing pending transaction remains inspectable offline.

Desktop-bearing intents use a new journal format version. Continue to encode
and verify existing filesystem/registry journals with their exact version-1
canonical representation and digest. Do not rewrite historical journals.
Prior or desired absence resets only the selected user override. Verify every
mutation through a fresh read, including recovery and explicit revert.

The first packet implements tasks 10.1–10.3 through the concrete preference's
generation, capture payload, detector boundary, collision checks, and standalone
verifier with an injected hermetic backend. Production desktop detection remains
unavailable and capabilities remain dark. This proves transaction integration,
not native behavior. Leave tasks 10.4, 10.5, and 10.9 open; add no helper or unused
KDE fields to this packet.

Use maintained GIO and libdconf APIs through a small packaged native helper.
The helper performs one-key inspection, typed read/write/reset,
and synchronization; Go owns authorization and transaction control.
[GIO user-value reads](https://docs.gtk.org/gio/method.Settings.get_user_value.html)
distinguish supported overrides from inherited defaults, including locked values.
GIO filters stored values through the schema and can hide a wrong-type value as
absence. First use libdconf's raw `DCONF_READ_USER_VALUE` operation and check the
stored type. Unsupported raw state prevents mutation; it never becomes absence.
The [dconf command](https://github.com/GNOME/dconf/blob/master/bin/dconf.c)
offers effective and default reads, which cannot prove that distinction.
The helper uses typed boolean access and json-glib, with no custom JSON or
GVariant text parser. Direct maintained C APIs avoid an unsupported DConf
introspection binding and a custom foreign-function layer.

This adds architecture-specific helper builds and GIO, libdconf, and json-glib
runtime dependencies for GNOME. Keep the Go engine's CGO-free build. Probe the
selected runtime explicitly. Missing dependencies or a different backend produce scoped desktop
unavailability; do not install them or launch a session during discovery.
Memory/keyfile test backends prove their boundary, never GNOME/dconf promotion.
The runtime-bundle packet owns helper builds, installation, lookup, and native
architecture/linkage evidence. This cost supplies exact prior-state reads that
the command pair and a GIO-only Python helper cannot provide. Load the helper
lazily; unrelated reads and old journals need no desktop runtime. An unresolved
pending desktop mutation retains the existing global mutation stop.

The native boundary is not ready until two read and mutation preconditions are
resolved. A maintained reader must prove that an existing backing database is
readable and structurally healthy before raw `NULL` means absence; libdconf
discards those read errors. A missing database can represent legitimate absence.
Do not write a GVDB parser. Also prove that the reader and the actual writer
resolve the same user namespace before mutation. A caller's configuration
directory can differ from the existing service's directory. Inspect ephemeral
identities if required, but do not persist them as the stable recovery scope.
An unreadable database or an unprovable writer scope reports unavailable.

Build the foundation against that concrete preference's workflow. Prove inherited
absence, explicit false, content drift, reconciliation, interrupted mutation,
third-value conflict, and revert while preserving an unrelated preference.
Then add KDE's own version-aware key boundary and collision rules. Distinct KDE
keys may coexist; whole-file and ancestor-directory ownership must conflict.
Keep both desktop capabilities dark until their separate native session gates
pass. No new CLI command, manifest version, combined profile, or permission is
introduced by this technical refinement.

## Risks / Trade-offs

- **[Curated mapping initially leaves apps unresolved]** → Keep unresolved items visible and useful, ship the high-value corpus first, and grow mappings from observed demand. Never trade correctness for a misleading coverage number.
- **[Native package queries report dependencies as user intent]** → Use explicit/manual package queries, user-facing desktop evidence, and fixture-locked filtering; report ignored counts for auditability.
- **[Equivalent app names mask edition or feature differences]** → Source-qualified aliases are reviewed one by one; ambiguous editions use separate portable IDs or remain unresolved.
- **[External package version differs from the release Nixpkgs version]** → Preserve source version as evidence, show the target input/ref, and do not claim byte-identical migration from a different manager.
- **[Home Manager conflicts with existing dotfile ownership]** → Detect external ownership from live activation plus missing Endstate history, refuse silent activation, and provide an importable artifact or explicit safe fallback.
- **[Module schema v3 multiplies curation/test surface]** → Keep one portable module, require explicit variants, validate every authored path/operation, and make the golden corpus release-blocking.
- **[Pinned Home Manager modules drift or hide complex generation logic]** → Generate a source-hashed frozen registry from the exact release input, fail closed on changed targets/declarations, require an explicit disposition, and prove every supported codec with source-to-target round trips.
- **[A syntactically parseable file is not necessarily a semantic inverse]** → Use typed inversion only for reviewed direct settings mappings; fall back to safe file placement or a curated codec rather than manufacturing option values.
- **[A bounded regular file can still contain credentials, commands, identities, or machine paths]** → Treat byte/size/symlink bounds as transport safety only; require semantic validation, narrower value ownership, visible capture-only status, or exclusion before advertising lifecycle support.
- **[Registry presence inflates apparent support without executable authority]** → Require registry and schema-v3 Linux variant agreement, content verification, and the complete lifecycle matrix before a row counts as supported.
- **[GNOME/KDE settings need a desktop session/DBus]** → Report the lane unavailable outside a usable session, never fail package capture solely for that reason, and require real-session acceptance before advertising it.
- **[Some Linux sessions have no usable systemd user manager]** → Advertise scheduling independently from core Linux readiness, make unsupported state explicit, never fall back silently, and keep all schedule mutations in user scope.
- **[Floating developer overrides leak into released captures]** → Release builds embed immutable defaults and record the effective inputs; CI fails release artifacts whose defaults are not immutable.
- **[The GUI appears easy because Tauri is cross-platform but packaging differs]** → Treat Linux bundling, executable permissions, AppImage/deb/rpm behavior, updater/signing, portals, and real rendered flows as a separate release gate.
- **[A stale task ledger causes duplicate or conflicting delegation]** → Reconcile every checkbox against branch evidence, split partial tasks, pin the audited matrix baseline, and rerun Stage 0 before launching lanes.
- **[Scope grows into “support all Linux”]** → Define the adapter and golden-corpus matrix explicitly; additional managers, desktops, and mappings are follow-up catalog work rather than blockers unless advertised.

## Migration Plan

1. Accept the audited commit as the implementation baseline, reconcile every task as complete/partial/missing, correct task 8.3's readiness overclaim, and serialize or supersede overlapping active OpenSpec changes before their shared surfaces are edited. Decision 16 records the verified overlap state and the surface rules that bind every packet until the implemented changes are archived.
2. Complete the three foundation packets: platform-qualified provenance/cross-platform mappings; immutable catalog and Provisioning Generation authority; remaining bounded discovery adapters and hostile/cross-source fixtures.
3. Integrate the stable discovery count/event/capabilities contract into deterministic capture/bundle behavior and run the first full engine regression boundary.
4. Establish joint registry/schema-v3 execution authority, semantic file/typed codec rules, content verification, external Home Manager ownership, and the required matrix readiness gate.
5. Run isolated adapter cohorts over their own modules/codecs/fixtures, then serialize registry, parity-matrix, and real-Home-Manager smoke integration. Keep every unproven action dark.
6. Extend the durable restore core with typed value identity/snapshots/journals/recovery. Implement GNOME and KDE in separate dependent packets and promote each capability only after its exact real-session gate passes.
7. Reconcile the active schedule change and public contract, preserve Windows through the shared structured scheduler interface, then implement and verify the systemd user backend and its real user-session lifecycle.
8. Ship complete Linux runtime bundles, required distro/no-Nix/amd64/arm64 checks, and the real Ubuntu capture → apply → verify → settings revert → package rollback journey. Update compatibility and public contracts only to the level proven by these gates.
9. Open and implement the dependent `endstate-gui` change with the same runtime assets, thin capability/result consumption, AppImage plus deb packaging, rendered DevTools QA, and installed-package E2E.
10. Announce Linux only after engine and GUI acceptance pass. Before announcement, rollback is disabling unproven capabilities and retaining the prior managed-profile behavior; legacy Windows and Nix manifests remain loadable throughout.
11. Create the separate macOS/nix-darwin OpenSpec change using the proven contracts and run its real-Mac acceptance with the user's tester before any macOS claim.

## Open Questions

No unresolved choice blocks the first hermetic engine packets. The following promotion facts remain deliberately evidence-bound downstream nodes rather than implementation-time guesses:

- Snap stays unadvertised or explicitly experimental until its hermetic fixture and real-Ubuntu source-contract gate pass; it does not delay dpkg, Flatpak, desktop, executable, or config discovery.
- GNOME and KDE start with capabilities dark. Their exact allowlists, supported value types, Plasma/GSettings tooling boundaries, and release ranges are recorded from the implementation fixtures plus current real-session evidence before each tier is promoted.
- The GUI change launches with AppImage plus deb. It separately decides and proves its advertised CPU architectures and updater/signing matrix; RPM remains a follow-on unless Fedora installed-package support is explicitly added to that change.
- Real Ubuntu, GNOME, KDE, systemd-user, arm64, and later macOS runners are external acceptance dependencies. Missing evidence keeps only the corresponding capability or architecture dark and never converts a failed gate into a waiver.
