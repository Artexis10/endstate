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

## Goals / Non-Goals

**Goals:**

- Make a fresh Endstate install useful on an ordinary existing Linux desktop, including one with no Nix installation or Endstate state.
- Detect explicitly installed/user-facing applications and supported configuration without flooding the user with dependency packages.
- Convert only verified mappings into portable, revision-pinned Nix intents and preserve the original discovery evidence separately.
- Capture and restore a release-quality initial settings catalog through explicit platform variants.
- Provide the same opt-in scheduled drift value loop on supported Linux desktops without introducing a resident agent.
- Give the GUI one stable, product-language journey for discovery, selection, capture, apply, verification, and rollback/revert.
- Reach journey parity with Windows and exceed it where Nix provides atomic package generations and stronger reproducibility.
- Establish a Darwin-compatible module/discovery shape that the later macOS/nix-darwin change can reuse.

**Non-Goals:**

- Reproduce every installed Linux dependency or claim complete coverage of every distribution/package manager.
- Mutate apt, dnf/RPM, pacman, Flatpak, or Snap in this change.
- Infer a Nix attribute from name similarity, query an untrusted online resolver during capture, or claim an external package's version is the desired Nix version.
- Reverse-engineer arbitrary `home.nix`, flakes, shell programs, or opaque application databases into declarative settings.
- Capture credentials, keyrings, browser profiles, histories, caches, databases, recent-item state, or machine-bound desktop state.
- Silently compose into, rewrite, or activate a user-owned Home Manager configuration.
- Ship macOS/nix-darwin support or the Linux GUI from this repository change. They receive dependent OpenSpec changes in their owning repositories.
- Support Linux schedulers other than systemd user timers in the first release; the capability remains visibly unavailable on hosts without a usable user manager.
- Use raw catalog count as the parity metric. A smaller, verified Linux catalog with the complete journey is more valuable than hundreds of nominal modules that cannot be discovered or restored safely.

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

This is stronger and more testable than comparing feature checkmarks or module counts. Linux exceeds Windows at the package layer once the journey passes because the desired set commits atomically, exact release inputs are recorded, and prior generations are addressable. Windows retains broader catalog coverage until Linux modules catch up; documentation must state both facts.

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

### 8. The initial settings catalog is curated around high-value portable state

The release-blocking application corpus covers the cross-platform concepts already represented by the Home Manager catalog where live state can be captured safely: Git, Bash, Zsh, SSH configuration without keys, tmux, direnv, Starship, fzf, zoxide, bat, eza, ripgrep, fd, Neovim, Helix, WezTerm, Kitty, Alacritty, GitHub CLI without authentication, lazygit, Jujutsu, Atuin without account/session material, and Yazi. A module may be capture/verify-only if safe automated restore is not yet justified, but that limitation is visible in discovery.

GNOME and KDE receive a separate value-scoped tier inside the same platform-module contract:

- GNOME captures an allowlist of schema/key values through an engine-owned GSettings boundary. Apply backs up the previous value, writes only the named value, verifies it, and revert restores or resets that value.
- KDE captures an allowlist of KConfig file/group/key values through version-aware engine operations. It merges only the named keys, backs up prior values/absence, verifies them, and reverts them.

Whole dconf databases, whole KDE config directories, accounts, keyrings, histories, recent files, device topology, and session/window state are forbidden. This mirrors the safety lesson from Windows registry value capture: value-level ownership is the only defensible OS-settings tier.

### 9. Scheduled drift uses a systemd user timer and the shared command contract

Extend the planned `schedule enable|disable|status|run` command family behind a scheduler interface rather than creating Linux-only commands. On Linux, `enable` writes inspectable user units under the resolved XDG systemd user directory and enables an `endstate-drift-check.timer`; the paired service invokes the short-lived engine `schedule run` and exits. The timer uses `Persistent=true` so a missed run is caught after the user manager resumes. `disable` disables the timer and removes only Endstate-owned generated units; `status` reconciles the saved Endstate schedule config with the actual user-manager state.

The Linux path never requires root and never creates a system unit. A working `systemctl --user` manager is required for `schedule.supported=true`; when unavailable, the feature reports unsupported with remediation and the rest of Linux remains ready. The service receives the same explicit root/manifest coordinates as the Windows task so GUI and scheduled invocations share state. No long-running Endstate process is introduced.

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

## Risks / Trade-offs

- **[Curated mapping initially leaves apps unresolved]** → Keep unresolved items visible and useful, ship the high-value corpus first, and grow mappings from observed demand. Never trade correctness for a misleading coverage number.
- **[Native package queries report dependencies as user intent]** → Use explicit/manual package queries, user-facing desktop evidence, and fixture-locked filtering; report ignored counts for auditability.
- **[Equivalent app names mask edition or feature differences]** → Source-qualified aliases are reviewed one by one; ambiguous editions use separate portable IDs or remain unresolved.
- **[External package version differs from the release Nixpkgs version]** → Preserve source version as evidence, show the target input/ref, and do not claim byte-identical migration from a different manager.
- **[Home Manager conflicts with existing dotfile ownership]** → Detect external ownership from live activation plus missing Endstate history, refuse silent activation, and provide an importable artifact or explicit safe fallback.
- **[Module schema v3 multiplies curation/test surface]** → Keep one portable module, require explicit variants, validate every authored path/operation, and make the golden corpus release-blocking.
- **[GNOME/KDE settings need a desktop session/DBus]** → Report the lane unavailable outside a usable session, never fail package capture solely for that reason, and require real-session acceptance before advertising it.
- **[Some Linux sessions have no usable systemd user manager]** → Advertise scheduling independently from core Linux readiness, make unsupported state explicit, never fall back silently, and keep all schedule mutations in user scope.
- **[Floating developer overrides leak into released captures]** → Release builds embed immutable defaults and record the effective inputs; CI fails release artifacts whose defaults are not immutable.
- **[The GUI appears easy because Tauri is cross-platform but packaging differs]** → Treat Linux bundling, executable permissions, AppImage/deb/rpm behavior, updater/signing, portals, and real rendered flows as a separate release gate.
- **[Scope grows into “support all Linux”]** → Define the adapter and golden-corpus matrix explicitly; additional managers, desktops, and mappings are follow-up catalog work rather than blockers unless advertised.

## Migration Plan

1. Correct contract/documentation drift: describe existing Linux support as managed-profile substrate support and remove claims that realizer capture is intentionally package-only where the implementation already attaches config finalization.
2. Add immutable release pins and the package identity catalog with validation, without changing capture behavior.
3. Add schema-v3 parsing, Windows-only legacy adaptation, platform-qualified fingerprints/provenance, and tests; keep Linux variants absent until the engine can safely execute them.
4. Add discovery adapters and the additive envelope/capabilities contract behind a Linux capability flag. Capture continues to write only resolved intents.
5. Add live Linux config matching/capture and the initial application corpus, then GNOME/KDE value operations. Turn on platform-module capability only after their scoped tests pass.
6. Add the systemd user-timer scheduler behind the shared schedule interface and verify its full user-scope lifecycle.
7. Promote the Ubuntu real-Nix/Home Manager journey to required and add distro/release-artifact gates.
8. Update compatibility/CLI docs and publish the stable engine contract. Open and implement the dependent `endstate-gui` Linux change.
9. Announce Linux only after the GUI packages and the complete acceptance journey pass. Rollback before announcement is disabling the advertised capability and retaining the prior managed-profile behavior; legacy Windows and Nix manifests remain loadable throughout.
10. Create the separate macOS/nix-darwin OpenSpec change using the proven contracts and run its real-Mac acceptance with the user's tester before any macOS claim.

## Open Questions

- Whether Snap belongs in the first advertised source matrix or remains detected-but-experimental depends on fixture and real Ubuntu results; its inclusion must not delay dpkg/Flatpak/desktop discovery.
- The first supported GNOME and KDE release ranges must be pinned from current CI/real-session evidence during implementation rather than inferred from file formats alone.
- The GUI's Linux distribution set (AppImage alone versus AppImage plus deb/rpm) is owned by the dependent GUI change; at least one installable package plus a portable artifact is required before announcement.
