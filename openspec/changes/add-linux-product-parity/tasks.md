## 1. Baseline and compatibility guards

- [ ] 1.1 Add failing command tests for Linux capture on a machine with no Nix executable, no Endstate profile, native package evidence, and live supported config
- [ ] 1.2 Add regression tests that current Endstate-profile capture, Home Manager history recovery, Windows Winget/Chocolatey capture, and Darwin Nix/Brew capture remain unchanged until their new lanes opt in
- [ ] 1.3 Add a catalog regression proving every schema-v1/v2 flat module is treated as Windows-only and cannot attach on Linux
- [ ] 1.4 Add a fixture-backed inventory of the current compatibility/CLI/roadmap claims that overstate ordinary-machine Linux capture, for correction when implementation lands

## 2. Immutable Nix inputs and package identity catalog

- [ ] 2.1 Add one release-owned pin manifest and loader for revision-locked Nixpkgs and compatible Home Manager inputs
- [ ] 2.2 Replace floating release defaults in `internal/realizer/nix` with the pin manifest while retaining explicit environment overrides
- [ ] 2.3 Resolve and record effective Nixpkgs/Home Manager revisions in apply plans and Provisioning Generations; add round-trip tests for defaults and overrides
- [ ] 2.4 Add `catalog/packages/<portable-id>.jsonc` schema/types with portable ID, display name, Nix target, source-qualified aliases, optional config-module association, and mapping revision
- [ ] 2.5 Implement strict package-catalog loading and diagnostics for duplicate IDs, duplicate source aliases, missing/invalid Nix targets, ambiguous desktop/executable aliases, and unknown fields
- [ ] 2.6 Add deterministic package-catalog serialization/revision tests and repo-root/release-artifact lookup for installed CLI and GUI layouts
- [ ] 2.7 Seed and review package mappings for the release-blocking Linux settings corpus plus the highest-value browser/editor/desktop applications needed for the acceptance journey
- [ ] 2.8 Add CI that evaluates every advertised Linux mapping against the exact release Nixpkgs revision and records architecture-specific unavailability without guessing a substitute

## 3. Discovery core and reconciliation

- [ ] 3.1 Add `internal/discovery` source, inventory-evidence, resolved-intent, unresolved-item, source-status, warning, and count types
- [ ] 3.2 Add a fixed-argv, read-only inventory-adapter interface and injectable command/filesystem seams; reject shell evaluation and unbounded raw output
- [ ] 3.3 Implement the discovery orchestrator with deterministic adapter ordering, unavailable/source-failure states, cancellation, and top-level failure only when no trustworthy selected source succeeds
- [ ] 3.4 Implement catalog resolution that preserves source evidence separately from portable attribute, immutable input, installable, and mapping revision
- [ ] 3.5 Implement deterministic evidence reconciliation by portable identity with source-qualified native refs and documented fact precedence
- [ ] 3.6 Implement explicit/user-facing filtering and ignored dependency/runtime counts so native dependency closures never become user application lists
- [ ] 3.7 Implement config-only settings candidates that remain selectable when package normalization is unresolved
- [ ] 3.8 Add hostile/malformed/large-output, case/locale, duplicate, ordering, privacy-redaction, and cancellation tests for the discovery core

## 4. Linux inventory adapters

- [ ] 4.1 Implement Endstate-profile and available user-profile Nix inventory adapters, preserving exact flakeref provenance when recoverable
- [ ] 4.2 Implement Debian/Ubuntu explicit-package discovery from `apt-mark showmanual` plus `dpkg-query` evidence with hermetic fixtures
- [ ] 4.3 Implement RPM-family user-installed discovery through the available DNF/RPM read-only query and a fixture-defined fallback
- [ ] 4.4 Implement Arch explicit-package discovery through `pacman -Qe` with hermetic fixtures
- [ ] 4.5 Implement Flatpak application discovery with runtime/base filtering and source-qualified app IDs
- [ ] 4.6 Implement Snap application discovery behind a capability state; keep it unadvertised if fixture and real-Ubuntu validation do not meet the source contract
- [ ] 4.7 Implement XDG desktop-entry discovery across user/system data roots with hidden/no-display filtering and deterministic desktop IDs
- [ ] 4.8 Implement catalog-declared executable and config-path evidence using engine-owned path coordinates and bounded filesystem checks
- [ ] 4.9 Add cross-source reconciliation fixtures for the same app installed through multiple managers plus desktop/config evidence

## 5. Linux capture and CLI contract

- [ ] 5.1 Route Linux capture through discovery even when Nix is unavailable; keep capture read-only and bootstrap-free
- [ ] 5.2 Reconcile the Endstate Nix profile with ordinary-machine evidence instead of restricting capture to `realizer.Current()`
- [ ] 5.3 Emit only reviewed resolved intents into manifest apps, using fully qualified immutable Linux Nix refs and retaining source versions only in discovery provenance
- [ ] 5.4 Preserve unresolved and config-only items in the capture result without fabricating installable/manual apps
- [ ] 5.5 Add the additive `discovery` result schema, stable reason codes, source statuses, settings actions, and discovered/resolved/selected/unresolved/ignored counts
- [ ] 5.6 Extend `capture --only`, `--update`, `--sanitize`, and share behavior to resolved Linux app IDs and explicit platform modules without leaking unselected path-only config
- [ ] 5.7 Add structured discovery progress/item events and product-language messages while keeping backend commands/raw output in details only
- [ ] 5.8 Advertise discovery schema/version, available source adapters, platform-module support, and immutable-pin state through capabilities
- [ ] 5.9 Add capture-output/bundle validation tests proving repeated identical source state yields byte-stable ordering and safe provenance
- [ ] 5.10 Reconcile the zero-app contract: allow validated settings-only artifacts, fail zero-useful-state captures with discovery counts, and retain bounded source-specific retry behavior

## 6. Platform-aware config module schema

- [ ] 6.1 Add schema-v3 `platforms` types and strict JSONC parsing while keeping schema-v1/v2 parsing byte-for-byte compatible
- [ ] 6.2 Reject schema-v3 modules that mix `platforms` with legacy flat executable declarations or omit an executable variant's realization strategy
- [ ] 6.3 Adapt legacy flat modules to Windows-only runtime variants and add full-catalog tests on Windows, Linux, and Darwin host seams
- [ ] 6.4 Add engine-owned home/XDG/platform path coordinates, documented XDG fallbacks, canonicalization, containment, symlink, and unsupported-shell-syntax validation
- [ ] 6.5 Make package/path instance detection select only the host platform variant and consume source-qualified discovery evidence
- [ ] 6.6 Qualify schema-v3 generation identity/fingerprints/history with platform and add deterministic collision/history tests
- [ ] 6.7 Persist and validate schema-v3 source-platform/variant provenance without machine-local roots; reject missing/inconsistent data without legacy fallback
- [ ] 6.8 Add same-platform resolution by default plus explicit source-to-target platform mappings/migrations and `platform_mapping_missing` behavior
- [ ] 6.9 Extend catalog diagnostics, module snapshots, validation-mode boundaries, and frozen-fixture tooling for schema-v3 variants

## 7. Live settings realization and ownership

- [ ] 7.1 Run trusted Linux platform-variant matching and config collection after package reconciliation so live settings capture works without Endstate/Home Manager history
- [ ] 7.2 Implement `home-manager` realization for typed settings and captured file placements through the existing inspectable generated-flake path
- [ ] 7.3 Implement `endstate-restore` realization through the existing consent, backup, staging validation, journal, verify, and revert pipeline on Linux paths
- [ ] 7.4 Detect external Home Manager ownership from live activation plus absent/mismatched Endstate provisioning provenance
- [ ] 7.5 Refuse silent generated activation over external Home Manager ownership and emit a stable ownership-action result in plan/dry-run/apply
- [ ] 7.6 Generate a stable inspectable/importable module for external Home Manager owners and test that Endstate never edits their source graph
- [ ] 7.7 Allow only explicitly declared safe `endstate-restore` fallbacks after consent; test no strategy inference and no duplicate target ownership
- [ ] 7.8 Extend Home Manager/restore rollback records and verification output with effective input revisions and platform-variant identity
- [ ] 7.9 Add cross-lane collision tests for Home Manager file placement, direct restore targets, shared modules, and existing user files

## 8. Curated Linux application settings corpus

- [ ] 8.1 Add and live-round-trip Linux variants for Git, Bash, Zsh, and SSH config while excluding credentials, private keys, histories, and agent state
- [ ] 8.2 Add and live-round-trip Linux variants for tmux, direnv, Starship, fzf, and zoxide
- [ ] 8.3 Add and live-round-trip Linux variants for bat, eza, ripgrep, and fd
- [ ] 8.4 Add and live-round-trip Linux variants for Neovim and Helix with plugin/cache/state exclusions
- [ ] 8.5 Add and live-round-trip Linux variants for WezTerm, Kitty, and Alacritty with portable-config-only boundaries
- [ ] 8.6 Add and live-round-trip Linux variants for GitHub CLI and lazygit while excluding authentication/session material
- [ ] 8.7 Add and live-round-trip Linux variants for Jujutsu, Atuin, and Yazi while excluding account, sync-session, history, cache, and machine-bound state
- [ ] 8.8 Add per-module discovery, capture, supported restore or explicit capture-only, verify, secret-boundary, revert/rollback, and cross-platform-mapping tests
- [ ] 8.9 Extend the real Home Manager smoke manifest to activate the entire advertised `home-manager` subset against the immutable release input pair

## 9. GNOME and KDE value-scoped settings

- [ ] 9.1 Add an injectable GSettings read/write/reset boundary with typed value serialization and usable-session detection
- [ ] 9.2 Implement GNOME value capture, prior-value/absence backup, apply, equality verify, and revert for named allowlisted schema/key coordinates
- [ ] 9.3 Curate the initial GNOME preference modules and reject whole-dconf, account, keyring, recent-item, device-topology, and session/window-state scope
- [ ] 9.4 Add an injectable, version-aware KDE KConfig file/group/key boundary with safe typed serialization and merge semantics
- [ ] 9.5 Implement KDE value capture, prior-value/absence backup, apply, equality verify, and revert while preserving co-resident keys
- [ ] 9.6 Curate the initial KDE preference modules and reject whole-directory, credential, history, recent-item, device-topology, and session/window-state scope
- [ ] 9.7 Add hermetic GNOME/KDE operation tests plus real current-release desktop-session acceptance; advertise each tier only after its real-session gate passes
- [ ] 9.8 Ensure unavailable desktop sessions yield a scoped settings warning and never block independent app/package capture

## 10. Linux scheduled drift checks

- [ ] 10.1 Reconcile with the active `scheduled-drift-check` change and extract/retain one scheduler interface and shared `schedule enable|disable|status|run` result contract
- [ ] 10.2 Implement a systemd user registrar with injectable command/filesystem seams and no sudo/system-scope path
- [ ] 10.3 Generate atomic, inspectable Endstate-owned service/timer units with explicit engine/root/manifest coordinates and `Persistent=true`
- [ ] 10.4 Make enable idempotently reassert changed executable/config paths, reload the user manager, and retain one active timer
- [ ] 10.5 Implement ownership-checked disable/cleanup that removes only generated Endstate user units and preserves shared schedule config/history
- [ ] 10.6 Reconcile saved intent with live enabled/active/next-trigger state in `schedule status`; surface missing/modified unit mismatches
- [ ] 10.7 Gate Linux schedule capabilities on a usable systemd user manager and return stable unsupported behavior without cron/profile/system-unit fallback
- [ ] 10.8 Add unit tests for quoting, hostile paths, partial registration, reload/enable failures, ownership conflicts, idempotence, disable, and unsupported hosts
- [ ] 10.9 Add a user-scope lifecycle smoke: enable, inspect units, force run, read last result, reassert, disable, and prove generated cleanup

## 11. Documentation, CI, and release truth

- [ ] 11.1 Update CLI/event/capabilities contracts with discovery, platform-module, ownership-action, immutable-input, and Linux schedule fields/reason codes
- [ ] 11.2 Update `docs/COMPATIBILITY.md` to distinguish substrate support, ordinary-machine journey readiness, settings-catalog breadth, atomicity, and independently available scheduling
- [ ] 11.3 Migrate unique durable Linux/macOS deferred contracts from the legacy roadmap into OpenSpec/current contracts, then retire the parallel roadmap section as current authority
- [ ] 11.4 Add Ubuntu/Debian, Fedora/RPM, and Arch discovery fixture/container jobs with deterministic golden results
- [ ] 11.5 Add a required capture job with no callable Nix binary and no Endstate state
- [ ] 11.6 Promote the Ubuntu real-Nix/Home Manager Linux leg from `continue-on-error` to required after its complete capture/apply/verify/revert/rollback journey passes
- [ ] 11.7 Add Linux amd64/arm64 release-binary bootstrap, catalog lookup, immutable-pin, and capture smoke coverage
- [ ] 11.8 Add release guards that refuse ordinary-machine Linux-ready claims when required discovery, settings, pins, desktop, schedule-as-advertised, or artifact gates are missing

## 12. Completion verification

- [ ] 12.1 Run scoped tests for every changed discovery, catalog, module, capture, Home Manager, restore, desktop-settings, and schedule package during implementation
- [ ] 12.2 Run `cd go-engine && go test ./...` at the completed engine boundary and compare failures with the pre-change baseline
- [ ] 12.3 Run real Ubuntu fresh-machine capture → apply → verify → settings revert → package rollback from the shipped Linux binary and preserve the output as PR evidence
- [ ] 12.4 Run real GNOME and KDE acceptance for the tiers advertised by capabilities; leave an unverified tier dark rather than waiving it
- [ ] 12.5 Run the systemd user-timer lifecycle on a supported Linux session and prove no system-scope units or lingering Endstate process exist
- [ ] 12.6 Run `openspec validate --all --strict --no-interactive` and all repository contract/doc checks
- [ ] 12.7 Re-run Windows capture/apply/config/verify/revert and Darwin Nix/Brew regression suites to prove the platform abstractions did not change existing behavior

## 13. GUI release and macOS follow-on

- [ ] 13.1 After the CLI envelope stabilizes, create a separate `endstate-gui` OpenSpec change for Linux engine/catalog bundling, executable permissions, platform onboarding, discovery selection, ownership/consent/errors, verify/revert/rollback, updater/signing, and package formats
- [ ] 13.2 Implement and test the Linux GUI against the bundled release engine; perform Chrome DevTools QA on every changed rendered flow and a real installed-package E2E before announcement
- [ ] 13.3 Require the GUI to consume capabilities/discovery results only and prove it contains no package-manager, Nix, module-catalog, or filesystem discovery logic
- [ ] 13.4 Publish the Linux release only after both engine and GUI acceptance gates pass; do not equate CLI artifacts alone with the product release
- [ ] 13.5 Create a separate `add-macos-product-parity` OpenSpec change reusing package discovery/platform modules/scheduler interfaces for Homebrew, Mac App Store, Home Manager, nix-darwin, launchd, Darwin GUI packaging, and signing
- [ ] 13.6 Run the macOS change on a real Mac with the user's tester before making a macOS product-parity claim
