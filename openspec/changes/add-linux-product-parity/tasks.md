## Execution contract

This ledger is reconciled to the audited implementation baseline
`5a597d1fdcd552e3bbc2ae58b43d4505f7fc383f`. At that baseline:

- the pinned Home Manager index contains 391 program candidates;
- the existing-application matrix contains 359 rows: 15 reported supported,
  38 adapter-required, 306 review-required, and 0 reviewed not-applicable;
- all 344 non-supported rows block parity readiness;
- the frozen registry contains 17 file-roundtrip entries, 1 curated codec,
  10 exclusions, and no typed-roundtrip implementation;
- only ripgrep has an explicit schema-v3 Linux application variant, so the
  15 reported-supported rows are not release-certified lifecycle adapters.

Checkboxes describe accepted task outcomes, not the presence of partial bytes.
Partially implemented former tasks have been split so completed foundations are
checked and remaining acceptance work stays open.

Every implementation packet SHALL:

- start from the named integration revision in an isolated worktree;
- use unique writable roots such as
  `GOCACHE=/tmp/endstate-lane-<id>-gocache`,
  `XDG_DATA_HOME=/tmp/endstate-lane-<id>-xdg-data`,
  `XDG_STATE_HOME=/tmp/endstate-lane-<id>-xdg-state`, and
  `TMPDIR=/tmp/endstate-lane-<id>-tmp`;
- demonstrate a named red-first test failing for the intended reason before
  implementation and passing afterwards;
- avoid OpenSpec edits, commits, merges, pushes, unrelated refactors, and shared
  generated-registry/matrix edits unless the packet explicitly owns them;
- receive a fresh author-independent review of the actual diff, including an
  adversarial or mutation test appropriate to the change;
- stop on contract ambiguity, unexpected shared-file ownership, unrelated
  baseline failure, or a required external environment that is unavailable.

Protected paths `go-engine/cmd/endstate/`, `docs/contracts/*.md`, and
`.github/workflows/` may be edited only by packets that explicitly name them.
Current `CLAUDE.md` governs repository authority; do not restore the retired `docs/ai` hierarchy. `LICENSE` and `NOTICE` remain forbidden.

The dependency graph is:

```text
1 plan/overlap reconciliation
 └─ 2 compatibility guards/product truth
      ├─ 3 immutable catalog/provisioning
      ├─ 4 discovery completion (after 3.3)
      └─ 5 platform provenance
           └─ 6 capture contract integration (after 3 + 4)
                ├─ 7 settings ownership/realization
                ├─ 8 registry authority/safety
                │    └─ 9 adapter cohorts + serialized registry integration
                ├─ 10 durable desktop values → GNOME/KDE
                └─ 11 shared scheduler → systemd user

9 + 10 + 11 → 12 release/CI truth → 13 engine acceptance
13 → 14 GUI delivery → Linux publication → macOS follow-on
```

Dispatch each task after the producers that it consumes are complete. Section
dependencies describe integration boundaries, not a blanket refusal to prepare
or test independent work against a pinned candidate. Cross-platform mapping
execution still requires accepted task 5.5; same-platform capture does not use
that result. Unreviewed changes remain ineligible for delivery. GNOME, KDE,
systemd-user, real-host, GUI, architecture-promotion, and macOS nodes may remain
explicitly blocked without preventing independent hermetic engine packets;
their capabilities and claims remain dark.

### Implementation priority: representative state lifecycle

Use decision 21 to sequence existing tasks; retain every task and release gate.

1. Prove one existing package and declared configuration through capture, preview, apply, content verification, drift, reconciliation, and resource-specific undo.
2. Complete the capture, ownership, and content-policy tasks that this proof exposes; reuse accepted platform and provisioning foundations.
3. Extend the journey with one allowlisted desktop preference and the existing drift schedule, preserving unrelated state and prior absence.
4. Expand adapter cohorts after the representative workflows establish their shared requirements, keeping native promotion evidence explicit.

The first implementation packet covers the needed parts of 6.4–6.8, 7.1,
7.3, 7.7–7.8, 8.4, 8.10, and the chosen configuration's reviewed policy from
9.2 or its semantic codec. Complete these tasks only when their full stated
outcomes pass. The desktop extension uses 10.1–10.5; the timer uses 11.1–11.9.
KDE, further cohorts, the final matrix, packaging, and GUI remain owned work.
No new generic resource framework or combined profile schema is required.

## 1. Plan, baseline, and overlapping-change reconciliation

**Packet type:** orchestrator-owned planning; no implementation delegation.

**Gate:** `npm run openspec:validate`, clean diff check, regenerated matrix
baseline, and a fresh lane-delegate Stage-0 readiness audit.

- [x] 1.1 Audit the branch against every prior task and record the independently reproduced 391-program and 359-row matrix baseline
- [x] 1.2 Reconcile proposal, design, delta specs, and this ledger with the execution-authority, immutable-input, count, desktop-transaction, scheduler, runtime-bundle, and GUI-package decisions
- [x] 1.3 Reconcile or explicitly serialize the active `scheduled-drift-check`, `catalog-discovery-hardening`, `home-catalog-data-driven`, and `verified-module-matrix` changes before touching their shared implementation surfaces
  - Resolved by explicit serialization: design decision 16 records the state of each change verified on 2026-09-01 against `5a597d1` and the surface rules R1–R6. The three implemented changes sync and archive in one cohesive documentation batch; `verified-module-matrix` stays active and is serialized by R1, R2, and R6.
- [x] 1.4 Re-run lane-delegate Stage 0 against the revised artifact digest and emit only dependency-ready packets with exact base revisions and file ownership
  - Recovery intake on 2026-11-04 integrates main `d76d3e5`, passes the four scoped Go packages and all 101 OpenSpec items, and reproduces 359 application rows with 344 blockers. An independent planning recheck permits tasks 1.5, 2.3, and 2.4; downstream archive and external acceptance holds remain. Each packet uses the committed integration revision.
- [x] 1.5 Create and land one independently reviewed archive documentation batch from current `origin/main`: reconcile the `scheduled-drift-check` ledger against the shipped implementation and archive it; archive `catalog-discovery-hardening`; sync and archive `home-catalog-data-driven` with 4.5 ticked and 4.4 carried by task 9.15 here. Sections 7.2, 9 (`home_catalog.go`), 11, and 12.6 dispatch only after the archive they depend on lands
  - PR #243 merged as `e9105c1` after independent review and all head CI checks. This branch integrates the three synced main specifications and archives. Remaining real-host, GUI, Home Manager activation, and release acceptance stays with its named tasks.

## 2. Compatibility guards and product-truth baseline

**Depends on:** 1.3 before shared contract/documentation edits.

**Red-first tests:** `TestCapturePlatformRegressionMatrix`,
`TestCompatibilityClaimsMatchLinuxJourneyEvidence`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-guards-gocache go test
./internal/commands ./internal/modules` plus the compatibility-claim fixture.

- [x] 2.1 Prove Linux capture with no callable Nix, no Endstate profile, native package evidence, and live supported config
- [x] 2.2 Prove schema-v1/v2 flat modules remain Windows-only and inert on Linux
- [x] 2.3 Add one regression matrix covering Endstate-profile capture, Home Manager history, Winget/Chocolatey, Darwin Nix/Brew, and explicit platform opt-in
  - Reuse the existing platform-specific suites. The new mixed-inventory command check reproduces and repairs lost Nix version evidence while preserving native-source version separation, Home Manager history, and selection.
- [x] 2.4 Add a fixture-backed inventory of compatibility, CLI, and roadmap claims that exceed current ordinary-machine evidence
  - `internal/linuxparity/testdata/compatibility-claims.json` records the inspected revision, source, scope, evidence, and remaining gap. Compatibility documentation now states the substrate boundary. The fixture is an evidence snapshot, not another release gate; no test pins Markdown wording.

## 3. Immutable inputs, package catalog, and provisioning authority

**Depends on:** section 2 complete after 1.3 catalog/bootstrap overlap
reconciliation.

**Shared-file rule:** design decision 16 R5. Task 3.6 does not reorder root
resolution.

**Packet boundary:** this packet owns release inputs, package-catalog
validation, Provisioning Generation schema, and mapping-evaluation CI. It does
not own capture envelopes or platform-module provenance.

**Red-first tests:** `TestProvisionGenerationRecordsEffectiveReleaseInputs`,
`TestCatalogRejectsAmbiguousDesktopAndExecutableAliases`,
`TestInstalledRuntimeCatalogRoundTrip`,
`TestAdvertisedMappingsEvaluateAtPinnedRevision`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-catalog-gocache go test
./internal/releaseinputs ./internal/packagecatalog ./internal/provision
./internal/commands -run 'Provision|Catalog|ReleaseInput'`.

- [x] 3.1 Add one release-owned revision-locked compatible Nixpkgs/Home Manager input manifest and strict loader
- [x] 3.2 Route Nix and generated Home Manager release defaults through that manifest while retaining explicit advanced override seams
- [x] 3.3 Add strict versioned package-catalog types, deterministic revisions, duplicate-source diagnostics, and safe Nix targets
- [x] 3.4 Seed reviewed package mappings for the release depth corpus plus high-value browser/editor/desktop applications
- [x] 3.5 Require every release/default/explicit flakeref and advanced override to produce the exact locked installable used for mutation, then persist requested intent, platform/config identity, effective inputs, and Home Manager generation in the revised Provisioning Generation contract
  - Independent review accepts the stored-state and source-provenance implementation through `28d93d6`, including failed retries and rollback-to-capture recovery. Release checks remain pending.
- [x] 3.6 Complete source-specific desktop/executable ambiguity diagnostics, deterministic serialization, and installed CLI/GUI runtime-layout lookup tests
  - Independent review at `00462db` accepts deterministic alias diagnostics and relocated executable lookup for CLI and GUI layouts. Native GUI packaging acceptance remains with section 14.
- [x] 3.7 Add required amd64/arm64 evaluation of every advertised Linux Nix mapping against the exact release revision, recording principled architecture unavailability without substitutes
  - Independent review accepts `8a9034b`. Native evaluation resolves all 45 catalog mappings on each architecture at the embedded revision and source hash. Metadata-declared unavailability remains distinct from evaluation failure. Required CI connects both evaluations to Go Tests; remote execution remains pending.

## 4. Discovery completion

**Depends on:** section 2 complete and 3.3 package-catalog authority.

**Packet boundary:** one cohesive discovery packet owns source types,
orchestration, adapters, and fixtures. Capabilities integration remains in
section 6.

**Red-first tests:** `TestSnapAdapterCapabilityContract`,
`TestExecutableAndConfigEvidenceUsesBoundedCoordinates`,
`TestCrossSourceFixtureReconcilesNativeDesktopAndConfig`,
`TestDiscoveryHostileLocaleCasePrivacyAndCancellation`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-discovery-gocache go test
./internal/discovery ./internal/packagecatalog`.

- [x] 4.1 Add source/evidence/intent/unresolved/status/count types plus a fixed-argv bounded read-only adapter and injectable runner
- [x] 4.2 Implement deterministic orchestration, source-scoped failures, cancellation, portable-identity reconciliation, explicit/user-facing filtering, and config-only candidates
- [x] 4.3 Implement Endstate/user Nix, Debian, RPM-family, Arch, and Flatpak inventory adapters with dependency/runtime filtering
- [x] 4.4 Implement deterministic XDG desktop-entry discovery with precedence and visibility filtering
- [ ] 4.5 Implement Snap behind an experimental/dark capability and promote it only after hermetic plus real-Ubuntu source-contract evidence
  - The dark adapter and hermetic contract are accepted at `f38461a`. Real-Ubuntu promotion remains open; it does not block other discovery sources.
- [x] 4.6 Implement catalog-declared executable/config-path evidence through bounded engine-owned coordinates
  - Independent review at `00462db` accepts metadata-only presence checks and their production capture integration. Review-only settings candidates grant no capture action, and explicit unsupported selections cannot silently disappear.
- [x] 4.7 Complete malformed/large-output, locale/case, privacy-redaction, cancellation, and retry-budget coverage
  - Independent assessment at `dd4af93` accepts the existing malformed-row, subprocess-output-bound, exact-case, privacy, cancellation, and bounded RPM fallback checks. Native Debian inventory agrees under C and en_US.utf8. Other native providers and release environments remain separate acceptance work.
- [x] 4.8 Add one hermetic multi-source fixture for the same app across native managers, desktop, executable, and config evidence
  - The fixture drives Debian, Flatpak, desktop, executable, and config adapters through reconciliation. It retains five source-qualified evidence records for one package and preserves native versions separately. Independent review at `00462db` accepts the result.

## 5. Platform schema, provenance, and cross-platform mappings

**Depends on:** section 2 complete and 1.3 verified-module-matrix overlap
reconciliation.

**Shared-file rule:** design decision 16 R1 and R2. Schema-v3 loader or
projection changes refresh validation sidecars and run
`cd go-engine && go test ./internal/validationmatrix`; edits to the matrix
harness (including `validationmode`) are named per file and limited to what
the projection requires.

**Packet boundary:** owns schema-v3 validation, platform projection,
fingerprints/history, capture provenance, and authored platform mappings.

**Red-first tests:** `TestAllLegacyModulesInertOnLinuxAndDarwin`,
`TestSchemaV3CaptureRequiresSourcePlatform`,
`TestSchemaV3FingerprintChangesByPlatform`,
`TestCrossPlatformRestoreWithoutMappingSkips`,
`TestPathCoordinateContainmentAndSymlinkPolicy`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-platform-gocache go test
./internal/modules ./internal/manifest ./internal/config ./internal/bundle
./internal/commands -run 'Platform|Provenance|Generation|Coordinate'`.

- [x] 5.1 Add strict schema-v3 `platforms` parsing and reject mixed flat/executable declarations or missing realization strategies
- [x] 5.2 Treat legacy schema-v1/v2 flat modules as Windows-only without mutating the loaded catalog
- [x] 5.3 Prove full-catalog Windows compatibility and Linux/Darwin inertness through host seams and frozen snapshots
  - Independent review accepts `c307a59`. Both command loaders preserve committed declarations and select 367 Windows, one Linux, and zero Darwin modules. The source catalog has 366 legacy modules and one authored platform module. This proves host-seam compatibility; native Windows execution remains separate.
- [x] 5.4 Qualify generation identity, fingerprints, history, manifest provenance, and validation with non-empty source platform/variant identity
  - Accepted after independent review at `dc4176b`. Ten affected packages and mixed valid/tampered capture-and-restore probes passed. Native Windows execution remains unverified.
  - Decision 19 includes flat schema-v3 normalization through `default/g1`, source-snapshot schema linkage, the same-platform guard from 5.5, required host-coordinate support from 5.6, and the complete event/strict-reader chain. Full authored cross-platform mappings and remaining path tooling stay separate.
- [ ] 5.5 Implement same-platform default resolution plus explicit source-to-target mappings/migrations and stable `platform_mapping_missing`
  - Decision 20 defines target-owned one-step mappings, pinned target identity, staging and committed validation, and target path policy. The same-platform refusal is part of 5.4; positive mappings require their own accepted implementation and lifecycle proof.
- [x] 5.6 Complete coordinate canonicalization, containment, symlink, unsupported-shell, diagnostics, validation-mode, and frozen-fixture tooling
  - Independent review and outcome verification accept `8b7f378`, including original-host guards and whole-instance frozen fixtures. Real Linux capture/restore, all XDG fixture roots, and the existing Studio One fixture pass. The authoring guide documents supported coordinates. Native Windows/Darwin execution and the later Linux matrix remain separate.

## 6. Linux capture and additive CLI contract

**Depends on:** 3, 4, and the platform producers that the task consumes.

Section 4's discovery implementation and coverage are prerequisites. Its Snap
promotion remains a separate hold under the discovery specification; Snap stays
dark while the other capture sources proceed. Same-platform capture uses the
accepted 5.1–5.4 and 5.6 foundations. Task 5.5 still needs acceptance for
cross-platform mapping execution and delivery of its changes; it does not
block independent capture accounting, selection, or event implementation.

**Packet boundary:** owns capture orchestration, discovery counts/actions,
events, capabilities, selection flags, and bundle determinism. It serializes
with later contract and GUI-consumer work.

**Red-first tests:** `TestLinuxDiscoveryCountsReconcile`,
`TestCaptureDiscoveryEmitsSourceAndItemEvents`,
`TestLinuxCaptureIdenticalDiscoveryPreservesPortableBundle`,
`TestLinuxCaptureUpdateSanitizeShareNeverStagesUnselectedPathConfig`,
`TestCaptureAdapterRetriesAreBounded`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-capture-gocache go test
./internal/commands ./internal/events ./internal/bundle ./internal/discovery`.
After independent review, run the first tranche boundary:
`cd go-engine && GOCACHE=/tmp/endstate-boundary-capture-gocache go test ./...`.

- [x] 6.1 Route Linux capture through ordinary-machine discovery without Nix bootstrap and reconcile Endstate-profile evidence
- [x] 6.2 Emit only reviewed resolved intents while preserving unresolved/settings-only state and allowing validated settings-only artifacts
- [x] 6.3 Expose initial discovery, platform-module, source-adapter, and immutable-input capabilities
- [x] 6.4 Implement the normative raw-source versus unique-candidate count model and independently checkable equations
  - Independent review accepts `9f32b03`: complete inventories, typed candidate identities, overlapping source observations, settings-only partition, and selected-candidate union. Selected/update/share/sanitize workflows preserve unselected inventory and scope private plans. Remaining historical Home Manager and catalog-backed collection guarantees stay with 6.5.
- [x] 6.5 Complete `--only`, `--update`, `--sanitize`, and share behavior without leaking unselected path-only settings
  - Independent review accepts `1fd680d`. Live collection and historical declarations obey selection, update, sanitization, and share boundaries. Retained declarations do not grant file-read authority or duplicate ownership. Failed collection preserves the previous artifact. Integrated regression checks pass at `f2f7de7`; fresh-host acceptance remains separate.
- [x] 6.6 Add stable discovery source/item events, product-language messages, reason codes, and details-only backend diagnostics
  - Independent review accepts `70e974d`, integrated as `3d506d1`, and the GUI contract mirror. Events reuse normalized rows; fatal errors and fallback retain source health without raw diagnostics. The integrated full suite passes with 5,493 named tests, 41 skips, and no failures. Rendered GUI acceptance remains separate.
- [x] 6.7 Prove repeated identical discovery preserves stable discovery, payload, and bundle structure with safe source provenance
  - Keep honest capture timestamps. Exclude only documented capture-time and archive-entry-time fields from equivalence checks; compare all other metadata and payload bytes. This requires no replay or frozen-time behavior.
  - Independent review accepts `TestLinuxCaptureIdenticalDiscoveryPreservesPortableBundle` at `1fd680d`. The production registry and native file capture preserve serialized discovery, archive structure, metadata, and payload. Capture timestamps fall within each observed run window; portable metadata excludes the source home. The integrated full suite passes at `f2f7de7`.
- [x] 6.8 Implement and test source-specific bounded retry reporting while retaining the zero-useful-state contract
  - Independent review accepts the existing RPM fallback's zero-or-one additional-query accounting on success and failure. Cancellation, malformed output, and output limits stop fallback. Independent adversarial probes preserve private details and publication refusal. The integrated full suite passes at `3d506d1`; this adds no generic retry policy or native provider claim.

## 7. Settings realization and ownership

**Depends on:** 5 and 6.

**Shared-file rule:** design decision 16 R4. Task 7.2 edits
`internal/realizer/nix/home_catalog.go` only after `home-catalog-data-driven`
is synced and archived (task 1.5).

**Packet boundary:** owns trusted platform-variant matching, Home Manager/direct
restore routing, external ownership actions, Provisioning Generation linkage,
rollback, and collision prevention. It does not add app-specific codecs.

**Red-first tests:** `TestLinuxVariantEndstateRestoreUsesConsentJournalAndRevert`,
`TestExternalHomeManagerNeverEditsSourceGraph`,
`TestHomeManagerAndRestoreTargetCollisionFails`,
`TestHomeGenerationRecordsInputRevisionsAndVariant`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-ownership-gocache go test
./internal/commands ./internal/bundle ./internal/realizer/nix
./internal/provision ./internal/modules ./internal/configrestore`.

- [ ] 7.1 Match and collect live settings only through trusted schema-v3 Linux variants after package/evidence reconciliation
- [ ] 7.2 Complete typed/file Home Manager generation and target-placement round-trip tests through the inspectable generated flake
- [ ] 7.3 Route explicit Linux `endstate-restore` variants through consent, backup, staging validation, durable journal, target verification, and revert
- [ ] 7.4 Fully classify external Home Manager ownership from live activation plus exact Endstate provisioning provenance
- [x] 7.5 Refuse silent generated activation over an external Home Manager owner and return an ownership action
- [ ] 7.6 Produce a stable importable artifact and prove Endstate never edits the owner's source graph
- [ ] 7.7 Reject strategy inference, undeclared fallbacks, and duplicate ownership across Home Manager, direct restore, shared modules, and existing files
- [ ] 7.8 Record effective inputs and platform/config identity in Home Manager/direct-restore verification, rollback, and Provisioning Generation output

## 8. Home Manager harvester, registry authority, and readiness gate

**Depends on:** 3, 5, 6, and 7. Section 8 serializes with
`home-catalog-data-driven` and `verified-module-matrix` before shared-file
edits (design decision 16 R1, R2, and R4).

**Packet boundaries:** harvester/drift; registry/module authority; matrix
readiness; runtime action/content verification. These may be separate only when
their shared files are exclusive.

**Red-first tests:** `TestNixTargetProberCancelsHungEvaluation`,
`TestRegistryCheckEmitsBoundedDeterministicDriftDiff`,
`TestRegistryRejectsSupportedAdapterWithoutLinuxExecutionAuthority`,
`TestProductionMatrixDoesNotCountLegacyWindowsModuleFromAdapterAlone`,
`TestVerifyHomeManagerFilesDetectsContentDriftWithinSameGeneration`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-registry-gocache go test
./internal/hmharvest ./internal/hmregistry ./internal/linuxparity
./internal/modules ./internal/discovery ./internal/bundle
./internal/realizer/nix ./internal/commands`.

- [x] 8.1 Implement the deterministic release-time pinned Home Manager harvester, fixed-argv pure probes, and separate registry-refresh CLI
- [ ] 8.2 Add probe timeout/cancellation plus automated proof that harvester/Nix evaluation is absent from runtime capture dependencies
- [x] 8.3 Implement strict frozen registry parsing for exact inputs, hashes, reviewed dispositions, target coordinates, ownership uniqueness, and fail-closed codec dispatch
- [ ] 8.4 Bind every non-excluded adapter to an explicit schema-v3 Linux platform variant, package/module identity, targets, strategy, actions, and secret/content policy
  - Decision 22 starts with one shared registry/module/generation authority join. Discovery, private collection, and the matrix use its result. Missing bindings remain review-only; stale bindings withhold only their adapter. Complete all remaining bindings before closing this task.
- [x] 8.5 Generate a deterministic matrix that accounts for all 359 existing application modules and reports supported/adapter/review counters
- [ ] 8.6 Add persistent reviewed applicability/exclusion authority and make the zero-blocker readiness predicate mandatory in release validation
- [ ] 8.7 Emit bounded deterministic declaration/option/type/target/source-hash diffs that invalidate prior review
- [x] 8.8 Implement bounded regular-file observation/staging mechanics with size, regular-file, hash, target, and symlink checks
- [x] 8.9 Implement explicit exclusion and curated-codec dispatch that rejects unknown runtime strategies
- [ ] 8.10 Make advertised capture/restore/verify/revert actions honest and verify expected target contents rather than generation identity alone

## 9. Typed, file, and curated adapter cohorts

**Depends on:** 8.4, 8.7, and 8.10.

**Shared-file rule:** cohort lanes own distinct codec files, schema-v3
`modules/apps/<id>` declarations, package mappings, and fixtures only. They
SHALL NOT edit `catalog/home-manager/registry.jsonc`, shared registry/runtime
dispatch, the production matrix test, or the real-Nix smoke. Section 9.13 owns
that serialized integration. Design decision 16 R1, R2, and R4 also bind every
cohort lane: each `module.jsonc` edit refreshes its validation sidecar, a new
module directory ships its sidecar and reports denominator drift, and
`home_catalog.go` is edited only after `home-catalog-data-driven` archives.

**Framework red-first tests:** `TestTypedRoundTripDecodeEmitRenderDecodeEquivalence`,
`TestRegistryRejectsSensitiveOrExecutableTargetWithoutSemanticCodec`,
`TestAdvertisedAdaptersLifecycleAndSecretBoundary`.

**Cohort gates:** each lane runs its named codec/module/discovery/bundle tests.
The serialized integration runs `cd go-engine && GOCACHE=/tmp/endstate-lane-adapters-gocache
go test ./internal/settingscodec ./internal/hmregistry ./internal/linuxparity
./internal/modules ./internal/discovery ./internal/bundle ./internal/realizer/nix
./internal/commands`.

- [ ] 9.1 Implement the typed-roundtrip framework and require decode → emit → Home Manager render → decode equivalence
- [ ] 9.2 Replace generic byte preservation with reviewed data-only file policy; route command/secret/account/machine-capable targets to semantic codecs, narrower values, capture-only, or exclusion
- [ ] 9.3 Harden Git parsing against opaque credential keys, executable transports, malformed quoted values, includes, aliases, auth, filters, drivers, and identity-sensitive state
- [ ] 9.4 Add schema-v3 Linux variants and lifecycle proof for Git, Bash, Zsh, and SSH without credentials, keys, histories, or agent state
- [ ] 9.5 Add schema-v3 variants/codecs for tmux, direnv, Starship, fzf, and zoxide with command/environment safety tests
- [ ] 9.6 Add schema-v3 variants/codecs for bat, eza, ripgrep, and fd with cross-platform mapping proof where supported
- [ ] 9.7 Add schema-v3 variants/codecs for Neovim and Helix excluding plugins, language-server secrets/commands, caches, and machine paths
- [ ] 9.8 Add schema-v3 variants/codecs for WezTerm, Kitty, and Alacritty excluding environment secrets, shell state, and machine-bound paths
- [ ] 9.9 Add schema-v3 variants/codecs for GitHub CLI, lazygit, and Jujutsu excluding authentication, accounts, helpers, and executable hooks
- [ ] 9.10 Add schema-v3 variants/codecs for Atuin and Yazi excluding accounts, sync sessions, history, VFS/remote-service state, caches, and machine identity
- [ ] 9.11 Promote additional Home Manager-only programs only after the identical schema-v3 authority, content, provenance, secret, and lifecycle gates pass
- [ ] 9.12 Close every applicable-Windows row with a verified Linux variant or persistent reviewed platform/safety exclusion; capture-only does not satisfy a safely restorable Windows row
- [ ] 9.13 Serialize accepted cohort entries into the registry, regenerate the matrix, independently recompute all counters, and reject any unreviewed count drift
- [ ] 9.14 Enumerate every advertised adapter in a table-driven capture/apply/content-verify/secret/revert/cross-platform lifecycle test so an untested advertised adapter fails
- [ ] 9.15 Make the exact advertised registry drive a required real Home Manager smoke against the immutable release pair with no generation-only fallback, including one real `home-manager switch` activation per `curatedTable` field kind (`kindNone`, `kindString`, `kindStringMap`, `kindAnyMap`, `kindStringSlice`) that asserts the generated `home.nix` statement and the activated target, carrying `home-catalog-data-driven` task 4.4

## 10. Durable desktop values, GNOME, and KDE

**Depends on:** 5, 6, and the durable ownership model in 7.3. GNOME and KDE
implementation packets may run in parallel only after 10.1–10.3 land.

**Red-first tests:** `TestTypedValueTransactionRecoversInterruptedMutation`,
`TestGSettingsUserOverrideVersusDefault`,
`TestGSettingsRevertRestoresAbsence`,
`TestKConfigPreservesCoResidentKeys`,
`TestKConfigRevertRestoresAbsentKey`,
`TestDesktopSessionUnavailableIsScoped`.

**Hermetic gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-desktop-gocache go test
./internal/configrestore ./internal/modules ./internal/commands
./internal/desktopsettings` (or the final package selected by implementation).
Promotion additionally requires the named real-session script for that desktop.

Decision 23 binds the first transaction extension to GNOME `clock-show-date`.
Include generation capture, the desktop detector, collision checks, and standalone
content verification in its concrete workflow. Preserve version-1 history.
Keep native helper packaging and real-session promotion as explicit dependent work.
The initial 10.1–10.3 packet uses an injected hermetic backend through that full
workflow. Production detection stays unavailable; 10.4, 10.5, and 10.9 stay open
until native database health and reader/writer namespace proof are implemented.

- [ ] 10.1 Extend durable configuration transactions with typed target identity, value/absence snapshots, atomic intent/journal state, verification, recovery, and revert
- [ ] 10.2 Define canonical bounded value encodings and stable unavailable/ownership/partial-failure reason codes shared by desktop adapters
- [ ] 10.3 Integrate typed-value operations with module authority, backup/recovery inspection, verifier dispatch, and collision detection
- [ ] 10.4 Implement injectable GSettings user-value/read/write/reset/session boundaries that distinguish overrides from inherited defaults
- [ ] 10.5 Curate the initial GNOME schema/key/type allowlist and complete capture/apply/verify/revert while rejecting whole dconf and forbidden state
- [ ] 10.6 Implement an injectable Plasma/KConfig-version-aware file/group/key boundary with absence/empty distinction, safe locking, and merge semantics
- [ ] 10.7 Curate the initial KDE file/group/key/type allowlist and complete capture/apply/verify/revert while preserving co-resident keys and rejecting forbidden state
- [ ] 10.8 Keep unavailable desktop sessions source-scoped and independent from package/application capture
- [ ] 10.9 Run current-release real GNOME and KDE session acceptance separately; record exact supported ranges and keep any unverified tier dark

## 11. Shared scheduler and Linux systemd user timer

**Depends on:** design decision 16 R3, which requires `scheduled-drift-check`
to have its ledger reconciled against the shipped implementation and to be
archived with its spec synced into `openspec/specs` through task 1.5; and
stable section-6 capabilities/result fields. Shared
interface extraction and systemd implementation are serialized.

Decision 24 uses the existing schedule store and one structured registrar.
The shared namespace lock prevents cross-root unit races. Pending intent and
per-unit hashes retain interruption evidence. Linux runs wait for registration's
configuration commit; Windows keeps its existing lock and rollback behavior.
Native lifecycle acceptance remains separate from the injected manager proof.

**Red-first tests:** `TestSchedulerBackendSelectionPreservesWindows`,
`TestSystemdExecStartEscapesHostilePathsWithoutShell`,
`TestSystemdOwnershipConflictRefusesOverwrite`,
`TestSystemdPartialRegistrationRecovers`,
`TestScheduleStatusReportsMissingModifiedAndLiveState`,
`TestLinuxManualRunAndDiscardAreSchedulerIndependent`.

**Gate:** `cd go-engine && GOCACHE=/tmp/endstate-lane-schedule-gocache go test
./internal/schedule` and
`cd go-engine && GOCACHE=/tmp/endstate-lane-schedule-gocache go test
./internal/commands -run 'Schedule'`. Promotion requires the real systemd-user
lifecycle script.

- [ ] 11.1 Author this change's `scheduled-drift-check` MODIFIED delta against the synced capability and modify the public CLI contract so Windows and Linux share `enable|disable|status|run|discard-upload`, backend-neutral results, and stable reason codes
- [ ] 11.2 Replace the Windows-shaped pre-quoted registrar with structured availability/enable/disable/inspect intent while preserving Windows behavior
- [ ] 11.3 Implement systemd-user availability and user-unit-directory resolution with no sudo, system unit, cron, or profile fallback
- [ ] 11.4 Generate atomic service/timer units with structured native escaping, explicit engine/root/manifest coordinates, `Persistent=true`, and ownership marker/version/content hash
- [ ] 11.5 Implement idempotent reassertion plus deterministic recovery from each unit-write, reload, enable/start, and config-persistence failure
- [ ] 11.6 Implement ownership-checked disable/cleanup and refuse silent overwrite/delete of user-modified or foreign units
- [ ] 11.7 Reconcile saved intent with availability, enabled/active/next-trigger, unit ownership/integrity, and last-run state
- [ ] 11.8 Keep manual status/run and durable Cloud replay/discard scheduler-independent; expose separate schedule-operation and registration capability fields, advertising registration only with a usable user manager
- [ ] 11.9 Run the real user-scope lifecycle: enable, inspect, force run, read result, reassert, disable, and prove generated cleanup with no system units or lingering process

## 12. Contracts, runtime artifacts, CI, and release truth

**Depends on:** stable producers from 6–11. Contract and workflow paths are
protected and must be explicitly assigned to these integration packets.

**Shared-file rule:** design decision 16 R2 for workflows and the matrix
harness, and R5 for task 12.6, which owns `bootstrap_unix.go` plus the shared
`installCatalog` in `bootstrap.go` and absorbs `catalog-discovery-hardening`
follow-up 5.2.

**Red-first tests:** `TestLinuxCapture_NoNixNoState`,
`TestDistroDiscoveryGolden`,
`TestRunBootstrap_UnixInstallsRuntimeCatalogs`,
`TestInstalledBinaryDiscoversCatalogWithoutEndstateRoot`, plus
`scripts/linux-release-contract.test.mjs` and
`scripts/linux-release-truth.test.mjs`.

**Gate:** scoped Go/Node tests during implementation; required named CI checks
for Ubuntu/Fedora/Arch discovery, no-Nix capture, real Nix journey, amd64/arm64
artifacts, promoted desktops/systemd, and readiness policy.

- [ ] 12.1 Publish final CLI, event, capabilities, ownership, immutable-input, platform-module, desktop, and schedule contracts from stable producer schemas
- [ ] 12.2 Correct `docs/COMPATIBILITY.md` to separate substrate support, ordinary-machine readiness, settings breadth, atomicity, and independent scheduling/desktop tiers
- [ ] 12.3 Move unique durable deferred Linux/macOS roadmap contracts into explicit follow-on OpenSpec changes or non-normative deferred sections, then retire the competing roadmap authority
- [ ] 12.4 Add byte-stable Ubuntu/Debian, Fedora/RPM, and Arch discovery fixtures/jobs
- [ ] 12.5 Make no-Nix/no-Endstate-state capture a required job
- [ ] 12.6 Ship deterministic Linux runtime bundles containing binary, modules, payload, package catalog, Home Manager registry, and schema identity; make Unix bootstrap install/resolve them
- [ ] 12.7 Add required shipped-artifact bootstrap/catalog/pin/no-Nix capture smoke on Linux amd64 and arm64
- [ ] 12.8 Promote the complete real Ubuntu capture/apply/content-verify/settings-revert/package-rollback journey from non-blocking smoke to required evidence
- [ ] 12.9 Add a machine-readable readiness policy that fails publication claims when any required matrix, discovery, codec, pin, artifact, real-host, or advertised capability check is absent

## 13. Engine completion and independent acceptance

**Depends on:** section 2 complete and all advertised section 9–12 nodes.
Unadvertised desktop, scheduler, Snap, or architecture nodes may remain dark
but cannot be waived if claimed.

- [ ] 13.1 Run every changed package's scoped gate and retain each independent reviewer verdict plus correction recheck
- [ ] 13.2 Run exactly one completed-engine `cd go-engine && GOCACHE=/tmp/endstate-boundary-final-gocache go test ./...` and compare any failure with the recorded baseline
- [ ] 13.3 Run the shipped-binary real Ubuntu fresh-machine journey and preserve capture → apply → target-content verify → settings revert → package rollback evidence
- [ ] 13.4 Run real GNOME and KDE acceptance for exactly the tiers advertised by capabilities
- [ ] 13.5 Run the real systemd-user lifecycle for advertised scheduling and prove no system-scope units or lingering process
- [ ] 13.6 Run `npm run openspec:validate`, all contract/doc/readiness checks, and regenerate the matrix with the mandatory readiness predicate
- [ ] 13.7 Re-run Windows capture/apply/config/verify/revert and Darwin Nix/Brew suites after the final abstractions
- [ ] 13.8 Obtain a fresh zero-context integration review over the combined diff and independently attack shared authority, secret, rollback, and release-gate boundaries

## 14. Linux GUI release and macOS follow-on

**Depends on:** stable engine contracts and accepted section 13. A separate
`endstate-gui` OpenSpec change owns GUI implementation.

**Initial packaging decision:** AppImage plus deb. RPM remains follow-on unless
the GUI change explicitly adds Fedora installed-package evidence. Advertise
only CPU architectures with packaged E2E and updater/signing proof.

- [ ] 14.1 Create the separate GUI OpenSpec change for runtime-asset bundling, executable permissions, Linux onboarding, discovery selection, ownership/consent/errors, verify/revert/rollback, AppImage/deb packaging, updater/signing, and release policy
- [ ] 14.2 Add GUI typed consumers and an architectural guard proving no package-manager, Nix, module-catalog, or filesystem discovery logic exists in GUI code
- [ ] 14.3 Bundle the version-matched engine, modules, payload, package catalog, Home Manager registry, and schema identity in development and packaged GUI layouts
- [ ] 14.4 Implement resolved/settings-only/unresolved/source-warning/external-Home-Manager/unsupported-schedule rendered flows and perform Chrome DevTools QA on every changed flow
- [ ] 14.5 Build AppImage plus deb, run the complete journey from installed/portable packages rather than a dev sidecar, and validate updater/signing metadata for every advertised artifact
- [ ] 14.6 Publish Linux only after both engine and GUI readiness policies pass; CLI artifacts alone do not constitute the product release
- [ ] 14.7 Create a separate `add-macos-product-parity` OpenSpec change for Homebrew/Mac App Store discovery, Home Manager/nix-darwin ownership, launchd, Darwin GUI packaging, signing, and updater behavior
- [ ] 14.8 Run the macOS change on a real Mac with the user's tester before advertising macOS product parity
