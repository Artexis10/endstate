## 1. Contracts and Test Fixtures

- [x] 1.1 Update the config-portability, capture-artifact, capture-bundle, restore-safety, profile, CLI JSON, event, and GUI integration contracts for config generations, bundle/manifest v2, legacy warnings, and transactional migration semantics.
- [x] 1.2 Add canonical schema-v1 module, schema-v2 stable-generation, side-by-side path, forward-migration, invalid-graph, legacy bundle, and bundle-v2 fixtures.
- [x] 1.3 Add manifest/bundle dispatch tests proving the new loader accepts v1 and v2 and rejects versions greater than 2, plus frozen legacy-decoder safety tests proving generated v2 manifests expose no flat restore path to generation-aware payloads while application declarations and explicitly represented legacy lanes may remain visible.
- [x] 1.4 Lock the new resolution/status/reason vocabulary in contract tests and confirm existing envelope fields remain backward compatible.

## 2. Module Schema v2 and Catalog Validation

- [x] 2.1 Add schema-v2 module types for instance detectors, config sets, generations, version selectors, validation declarations, and migration edges while preserving the schema-v1 adapter.
- [x] 2.2 Implement canonical parsed-JSON serialization, SHA-256 module revision hashing, and per-generation fingerprints with tests for cosmetic versus semantic edits.
- [x] 2.3 Validate stable IDs, positive unique generation order, config-set scoping, allowlisted placeholders, and portable/staging-safe paths.
- [x] 2.4 Validate numeric version ranges and anchored raw-version patterns without declaration-order tie-breaking.
- [x] 2.5 Validate migration graphs for unknown generations, duplicate/same/backward edges, cycles, ambiguous routes, unknown operations, and missing validation.
- [x] 2.6 Replace silent catalog skipping for generation-aware module errors with structured diagnostics usable by commands and envelopes.
- [x] 2.7 Add table-driven catalog tests covering valid schema v1/v2 modules and every schema/graph/path rejection.
- [x] 2.8 Add released-generation fingerprint history/CI validation and explicit historical-fingerprint acceptance declarations so generation IDs cannot be silently repurposed.

## 3. Instance Discovery and Version Normalization

- [x] 3.1 Preserve captured `App.Version` through module matching and add regression tests for the current version-dropping path.
- [x] 3.2 Implement raw vendor-version preservation and the documented numeric dotted normalizer/comparator.
- [x] 3.3 Implement the package instance detector across engine-supported package backends with deterministic evidence and stable instance IDs.
- [x] 3.4 Implement the path instance detector with engine-owned glob expansion, named version extraction, deterministic ordering, and deduplication.
- [x] 3.5 Implement allowlisted `${instance.*}` expansion and reject missing values, absolute portable destinations, and traversal.
- [x] 3.6 Add side-by-side and irregular-version tests proving the engine never silently selects a newest instance.

## 4. Bundle v2 and Capture Provenance

- [x] 4.1 Add manifest-v2 `configCaptures[]` (including source-generation fingerprints) and metadata-schema-2 types with strict validation and v1/v2 loading dispatch.
- [x] 4.2 Give each captured config set a deterministic capture ID and isolated `configs/<captureId>/` payload root.
- [x] 4.3 Preserve full nested relative paths during collection and reject duplicate destinations, fixing basename collapse and collision behavior.
- [x] 4.4 Generate per-file size/SHA-256 payload manifests and verify them before compatibility planning.
- [x] 4.5 Write canonical source-module snapshots under `provenance/modules/`, verify their recorded hashes, and prevent them from becoming target authority.
- [x] 4.6 Emit manifest version 2 only when generation-aware config is present and ensure generation-aware sets have no flat restore fallback.
- [x] 4.7 Extend capture JSON output with bundle/manifest versions and per-instance, per-config-set provenance while preserving existing module metadata.
- [x] 4.8 Add bundle round-trip tests for nested paths, same-basename files, side-by-side instances, hash mismatch, edited module snapshots, mixed v1/v2 modules with isolated legacy entries, and install-only bundles.

## 5. Generation and Target Resolution

- [x] 5.1 Add normalized source-capture, target-instance, config-resolution, reason, and summary data types.
- [x] 5.2 Implement exactly-one generation matching and the `unknown_generation` / `ambiguous_generation` outcomes.
- [x] 5.3 Implement same-generation direct resolution and forward-route lookup against one pinned trusted catalog snapshot.
- [x] 5.4 Implement downgrade rejection, missing-path handling, unsupported-module-schema handling, payload-integrity failure, missing catalog module/set/generation, and changed/unaccepted source-generation fingerprint outcomes.
- [x] 5.5 Implement deterministic target selection: unique target, unique exact-version preference, explicit mapping, and ambiguous-target refusal.
- [x] 5.6 Detect exact and parent/child target collisions plus multiple captured sources competing for one target.
- [ ] 5.7 Re-run target detection after rebuild installs and ensure final resolution replaces stale pre-install evidence before restore.
- [ ] 5.8 Adapt legacy bundle/module restores to emit `legacy_unverified` without fabricating versions or generations.
- [ ] 5.9 Add planner tests for every resolution/reason, generation-fingerprint acceptance/rejection, side-by-side mapping, post-install version changes, collisions, per-set isolation, no legacy fallback, and catalog pinning.

## 6. Forward Migration Engine

- [x] 6.1 Add an engine-owned migration operation registry with no shell, command, executable, plugin, generic-regex, or host-absolute escape hatch.
- [x] 6.2 Implement staging-relative `file-copy`, `file-move`, and `file-delete` with traversal and symlink/reparse-point containment checks.
- [x] 6.3 Implement parsed `json-set`, `json-delete`, and `json-move` operations with atomic staged writes.
- [x] 6.4 Implement parsed `ini-set`, `ini-delete`, and `ini-move` operations with deterministic encoding/newline behavior.
- [x] 6.5 Implement `file-exists`, `json-parse`, `json-path-exists`, `ini-parse`, and `ini-key-exists` validation primitives.
- [x] 6.6 Implement unique multi-edge migration execution with validation after every edge and final-generation validation.
- [x] 6.7 Prove with tests that source bundle bytes remain unchanged on success and every failure path.
- [x] 6.8 Add negative security tests for arbitrary code declarations, absolute/traversal paths, links escaping staging, malformed documents, and unsupported binary formats.

## 7. Transactional Restore, Journal, and Revert

- [ ] 7.1 Convert a resolved direct/migrated config set into concrete restore actions only after staging and preflight succeed.
- [ ] 7.2 Pre-create all required backups for a config-set transaction before its first target write.
- [ ] 7.3 Add atomic journal-intent persistence before mutation and make journal write failure fatal for the config set/command.
- [ ] 7.4 Implement config-set commit, final target validation, atomic completion recording, and immediate rollback on partial commit, validation failure, or completion-record failure.
- [ ] 7.5 Extend journal entries with capture/target IDs, source/target generations, migration path, both module revisions, validation status, and rollback outcome.
- [ ] 7.6 Update revert to consume concrete journal actions without attempting a reverse generation migration.
- [ ] 7.7 Enforce application-closure requirements by returning `app_running` without killing processes or mutating the set.
- [ ] 7.8 Add failure-injection tests for backup, intent write, each commit action, final validation, rollback, and completion write.
- [ ] 7.9 Add multi-set tests proving a failed set rolls back while safe non-overlapping sets continue.
- [ ] 7.10 Detect pending journal intents before new mutation, perform idempotent recovery rollback, and block with `recovery_required` when recovery cannot complete.
- [ ] 7.11 Add crash-window and next-run recovery tests for process death before/during/after commit and incomplete completion records.

## 8. CLI, Envelopes, Events, and Capabilities

- [ ] 8.1 Add repeatable `--restore-target <captureId>=<targetInstanceId>` parsing to apply, restore, and rebuild, with input errors for malformed/duplicate/unknown-capture mappings and per-set skips for post-install absent/incompatible targets.
- [ ] 8.2 Advertise `--restore-target` through capabilities and preserve module-level `--restore-filter` precedence.
- [ ] 8.3 Add identical `configResolutions[]` and `configResolutionSummary` semantics to apply, standalone restore, and rebuild dry-run/live JSON output, including legacy warning states and source-generation fingerprints.
- [ ] 8.4 Link concrete `restoreItems[]` to capture IDs, config sets, target instances, and generations without changing app `items[]`.
- [ ] 8.5 Emit ordered `config-resolution` and `config-migration` JSONL events before target mutation, including validation/commit/rollback outcomes.
- [ ] 8.6 Add stable error/reason codes and remediation for invalid mappings, integrity failures, ambiguous instances/generations, target collisions, unsupported downgrade/schema, journal failure, and app-running state.
- [ ] 8.7 Add CLI, envelope, event-ordering, capabilities, dry-run, and legacy compatibility tests for all restore-capable commands.

## 9. Representative Modules and GUI Consumption

- [ ] 9.1 Convert one stable-layout module to schema v2 with one generation and prove direct restore remains simple.
- [ ] 9.2 Convert one versioned-directory module to path instance discovery and prove side-by-side capture/selection works.
- [ ] 9.3 Add one representative JSON or INI module with a real `g1 -> g2` migration and edge/final validation.
- [ ] 9.4 Add catalog-wide validation tests that schema-v1 modules remain valid and schema-v2 generation IDs/rules are safe.
- [x] 9.5 Update the GUI integration contract so the GUI renders engine-provided Compatible, Will be upgraded, Compatibility unknown, and Not supported states without recomputation.
- [ ] 9.6 Implement the separate GUI consumer for config resolutions, legacy warning/consent, side-by-side target selection, migration progress, and advanced provenance details.
- [ ] 9.7 Add GUI tests for all four distilled states, target-selection ambiguity, legacy restore consent, migration failure/rollback, and progressive disclosure.

## 10. Verification and Rollout

- [ ] 10.1 Run targeted Go tests for modules, bundle, manifest, planner/resolution, migration operations, restore/journal/revert, commands, envelopes, and events.
- [ ] 10.2 Run OpenSpec strict validation and contract/schema fixture validation.
- [ ] 10.3 Run end-to-end v1 legacy, v2 direct, v2 forward-chain, side-by-side ambiguous/selected, tampered payload, and rollback scenarios in temporary roots.
- [ ] 10.4 Run the full Go test suite after targeted suites pass and record any environment-specific exclusions.
- [ ] 10.5 Document module-author guidance for generations, immutable IDs, version selectors, detectors, migration operations, validation, and unsupported formats.
- [ ] 10.6 Release engine read support and structured contracts before enabling GUI v2 capture/restore; retain v2 read support if capture must be rolled back.
