## 1. Inspection model and command

- [ ] 1.1 Add the `profile inspect <manifest-path> --json` subcommand under the existing top-level `profile` command and reject missing, bundle, and directory inputs with structured failures.
- [x] 1.2 Implement a read-only manifest inspection path with restricted root-contained relative includes, root-only composition exclusions, permitted artifact metadata/snapshots, and the pure trusted-catalog enrichment loader.
- [x] 1.3 Derive v1 and v2 settings ownership as the specified canonical-key union, preserving provenance/raw IDs and excluding only root-config exclusions before association.
- [x] 1.4 Implement tiered verified-owner-ref selection, duplicate-safe Apps row identities, mandatory grouping, fixed status/identifier/nullability semantics, exact v1/v2 captured-entry counting, label/timestamp precedence, deterministic ordering, typed warnings, and post-grouping summaries.

## 2. JSON and capabilities contract

- [ ] 2.1 Add the schema-1.x `profile` success envelope data shape for inspection with non-null ordered arrays and nullable fields where specified.
- [ ] 2.2 Advertise `features.profileInspection: true` through capabilities without redesigning the generic command schema.
- [ ] 2.3 Return standard manifest error envelopes and a structured usage failure for a missing inspect path.

## 3. Verification

- [ ] 3.1 Add hermetic engine tests for extracted-only/root-contained includes, no machine evaluation, root-only excludes, v1/v2 ownership unions, tiered owner-ref association, duplicate Apps ambiguity/identities, mandatory grouping, status matrix/IDs, labels/timestamp, valid unique-capture entry counts, warnings, ordering, and summaries.
- [ ] 3.2 Add schema/envelope tests for success, manifest failures, missing-path usage failure, and capability advertisement.
- [ ] 3.3 Update GUI integration coverage to capability-gate the feature, render engine semantics, and show update-required for stale engines without fallback ownership inference.
- [ ] 3.4 Run targeted engine tests, GUI tests, and strict OpenSpec validation.
