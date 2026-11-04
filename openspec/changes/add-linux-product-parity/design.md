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
