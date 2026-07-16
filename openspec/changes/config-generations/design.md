## Context

Config modules are currently flat: one module supplies capture paths and restore targets for an application, and capture bakes those targets into `manifest.jsonc`. The engine models package versions but does not record the application version or config layout that produced a payload. Restore therefore cannot distinguish a direct restore from a format change, a moved config tree, an unsupported downgrade, or an unknown combination.

The implementation also has two substrate gaps that this change cannot build around: wildcard capture paths are treated as literal paths, and bundle source rewriting can collapse nested paths to a basename. Both prevent reliable discovery and preservation of versioned config directories.

The engine remains the source of truth. Modules are declarative catalog data, the GUI presents engine decisions, and capture bundles are portable payload plus provenance. Existing restore consent, backup-before-overwrite, journaling, and revert guarantees remain in force.

## Goals / Non-Goals

**Goals:**

- Model application config compatibility per independently evolving config set.
- Distill vendor version complexity into stable configuration generations.
- Capture multiple side-by-side application/config instances without silently choosing one.
- Resolve direct restore, forward migration, incompatibility, ambiguity, and unknown state before config mutation.
- Make forward migration deterministic, declarative, staged, validated, journaled, and reversible.
- Preserve immutable source provenance while allowing the trusted current catalog to add knowledge about newer targets.
- Keep legacy modules and bundles usable without pretending their compatibility is verified.
- Give the GUI a small structured result vocabulary while retaining inspectable technical detail.

**Non-Goals:**

- Arbitrary shell, PowerShell, batch, executable, plugin, or bundle-supplied migration code.
- Automatic backward migration. Reverse paths require a later design and separately proven losslessness.
- Guessing migrations for opaque or proprietary binary formats.
- Promising support for every historical or future application version.
- Making the GUI detect versions, resolve compatibility, or interpret migration rules.
- Signing or encrypting capture bundles; payload hashes provide integrity checks, not authenticity.
- Requiring the entire existing module catalog to adopt generation metadata at once.

## Decisions

### 1. Compatibility is between config generations, not application versions

Each module schema-v2 `configSet` represents a setting family that evolves independently, such as `preferences`, `presets`, or `workspaces`. Every set defines stable generations. An application instance may therefore use `preferences/g2` and `presets/g1` at the same time.

Application versions are detection evidence used to select a generation. They are not used as a pairwise compatibility matrix. Same-generation transfer is direct. Different generations require an explicit directed migration path. No path means the engine reports `incompatible` or `unknown`; it never infers compatibility from version ordering alone.

Generation IDs are permanent within `<moduleId>/<configSetId>`. A released ID cannot be reused with a different meaning. The engine computes a canonical fingerprint for each generation definition, and capture records that fingerprint alongside the ID. A current catalog whose same-named generation has a different fingerprint must explicitly accept the historical fingerprint; otherwise resolution is `unknown` with `source_generation_definition_changed`. Each generation also has a monotonically increasing `order`; order proves direction but does not create a migration edge.

This avoids both rejected alternatives: an N-by-N application-version matrix, and duplicated modules for every application version.

### 2. Module schema v2 is additive and progressively disclosed

Existing modules remain valid as implicit schema v1. They keep their flat capture/restore behavior and are classified as unversioned rather than being falsely relabeled as a universal `g1`.

Generation-aware modules declare `moduleSchemaVersion: 2` and an optional `config` block shaped conceptually as follows:

```jsonc
{
  "moduleSchemaVersion": 2,
  "config": {
    "instanceDetectors": [
      { "id": "installed-package", "type": "package" },
      {
        "id": "versioned-profile",
        "type": "path",
        "glob": "%APPDATA%\\Vendor\\App *",
        "versionPattern": "^App (?P<version>[0-9.]+)$"
      }
    ],
    "sets": [
      {
        "id": "preferences",
        "displayName": "Preferences",
        "generations": [
          {
            "id": "g1",
            "order": 1,
            "matches": [{ "versionRange": ">=25 <28" }],
            "acceptsSourceFingerprints": [],
            "capture": {},
            "restore": [],
            "validate": []
          }
        ],
        "migrations": []
      }
    ]
  }
}
```

Generation capture/restore definitions reuse existing declarative primitives. Paths may additionally use allowlisted instance placeholders such as `${instance.root}`, `${instance.version}`, and `${instance.id}`. Unknown placeholders and path traversal are catalog errors.

The catalog computes a module revision as SHA-256 over canonical parsed JSON, excluding loader-only fields. It also computes each generation fingerprint over the canonical generation definition. Comments, whitespace, and line endings therefore do not create fake revisions. Authors do not maintain revision numbers by hand. Catalog CI retains released generation fingerprints and rejects reuse unless the new definition explicitly accepts the historical fingerprint.

### 3. Instance detection is engine-owned and returns zero, one, or many instances

The first release supports two detector types:

- `package` reuses matched installed-package records and preserves the package backend, ref, raw version, and normalized version.
- `path` expands an engine-owned glob, optionally extracts a version with a named regex capture, and exposes the matched root through `${instance.root}`.

This explicitly fixes the current literal-wildcard behavior. Detector output is normalized, deduplicated, and deterministically sorted. Each instance receives a stable ID derived from the module ID, detector ID, and canonical non-secret locator. Raw vendor versions are always preserved.

The comparator operates on numeric dotted versions extracted by the detector. A generation match can use a numeric `versionRange` or an anchored `versionPattern` against the raw value. Exactly one generation must match each config set. Zero matches is `unknown_generation`; multiple matches is `ambiguous_generation`. The engine never uses declaration order as a hidden tie-breaker.

Side-by-side results remain separate. The engine automatically maps a captured config set only when there is one compatible target or one exact-version target. Multiple viable targets produce `ambiguous_target_instance` until the caller supplies an explicit target mapping.

### 4. Generation-aware bundles use v2 provenance and structural isolation

A generation-aware capture writes bundle metadata schema `2.0` and an embedded manifest version `2`. New engines explicitly dispatch and validate manifest/bundle v1 and v2. Released legacy engines do not reliably reject an unknown manifest version: they may still process application declarations and existing explicit legacy lanes. Safety for generation-aware configuration therefore comes from structural isolation, not legacy version rejection. Generation-aware payloads exist only in `configCaptures[]`, which legacy engines do not interpret, and never have a flat restore path. Legacy engines consequently cannot execute those payloads, while version-aware bundles never fall back to flat blind restore for them.

The v2 manifest contains `configCaptures[]`. Each record is one captured config set and includes:

```text
captureId
moduleId
configSetId
sourceInstance { id, detectorId, rawVersion, normalizedVersion, evidence }
sourceGeneration
sourceGenerationFingerprint
captureModule { schemaVersion, contentHash, snapshotPath }
payloadRoot
payloadManifest[] { relativePath, size, sha256 }
```

Payloads live under `configs/<captureId>/` and preserve their complete relative hierarchy. Capture rejects duplicate destinations instead of collapsing nested paths. A canonical, non-executable snapshot of the source module is stored under `provenance/modules/` for inspection and hash verification; restore never treats that snapshot as target authority.

The bundle owns immutable source facts and bytes. The trusted catalog loaded by the current engine owns target discovery, target generation definitions, and current migration edges. A changed module hash is expected and is recorded, not treated as an automatic failure. A changed source-generation fingerprint requires an explicit acceptance declaration in the current catalog. Captured source facts are never rewritten to match the current module.

All payload hashes are checked before planning mutation. They detect corruption or internal inconsistency, not malicious re-signing of an editable bundle.

### 5. Resolution produces a first-class plan before config mutation

At run start the engine loads and pins one catalog snapshot in memory. Resolution consumes bundle source records, installed or planned application versions, discovered target instances, explicit target mappings, and that catalog snapshot.

For each captured config set it produces:

```text
captureId
targetInstanceId
sourceGeneration
sourceGenerationFingerprint
targetGeneration
resolution
reason
migrationPath[]
captureModuleRevision
restoreModuleRevision
resolvedTargets[]
```

`resolution` is one of `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified`. More precise causes live in `reason`, including `downgrade_unsupported`, `migration_path_missing`, `ambiguous_target_instance`, `ambiguous_generation`, `target_not_detected`, `target_collision`, `app_running`, `payload_integrity_failed`, `unsupported_module_schema`, `catalog_module_missing`, `config_set_missing`, `source_generation_unknown`, and `source_generation_definition_changed`.

Target detection runs before restore and, in rebuild, again after application installation because an unpinned or previously absent app may not have a knowable target generation earlier. Application installation is independent: incompatible, unknown, or ambiguous config sets are skipped before config mutation while app installation and unrelated config sets continue.

Before execution, the planner rejects overlapping target paths across selected config sets, including parent/child overlaps. It also rejects multiple captured sets competing for one target. No “newest version wins” rule exists.

Existing module-level `--restore-filter` remains. A repeatable `--restore-target <captureId>=<targetInstanceId>` selection is added to restore-capable commands and advertised through capabilities. Malformed/duplicate mappings and mappings to an unknown capture ID are command-input errors before installation or config mutation. A syntactically valid mapped target that is absent or incompatible after final post-install detection skips only that config set with `mapped_target_not_detected` or `mapped_target_incompatible`; successful application installation remains intact.

### 6. Forward migration is an explicit, uniquely resolvable graph

Each migration edge names one source and one higher-order target generation within the same config set. Catalog validation rejects missing generations, duplicate edges, same/backward edges, cycles, ambiguous routes, unknown operations, and unsafe paths. In the first release there may be at most one route between any reachable source/target pair; this removes the need for hidden priorities.

Multi-step forward chains such as `g1 -> g2 -> g3` are supported. Every edge declares operations and validation. The initial engine-owned operation allowlist is deliberately small:

- `file-copy`, `file-move`, and `file-delete` within staging.
- `json-set`, `json-delete`, and `json-move` for parsed JSON documents.
- `ini-set`, `ini-delete`, and `ini-move` for parsed INI documents.

No generic command, regex replacement, or host-path write operation exists. All operation paths are relative to the config-set staging root. Initial validation primitives are `file-exists`, `json-parse`, `json-path-exists`, `ini-parse`, and `ini-key-exists`. Unsupported binary/XML transformations remain explicitly incompatible until the engine gains an appropriate reviewed primitive.

### 7. Execution is staged and config-set transactional

The engine copies the captured config set into a fresh staging directory, verifies payload integrity, applies each migration edge, validates each edge output, and validates the final target generation. The captured payload is read-only throughout.

Only after staging succeeds does the engine resolve concrete target actions. It refuses to stop or kill a running application; a module that requires closure produces `app_running` and no target write.

For each config set the engine then:

1. Creates all required backups.
2. Atomically persists a journal intent containing the plan and backup locations.
3. Commits resolved restore actions.
4. Verifies the committed target generation.
5. Atomically marks the journal entry complete.

If commit, verification, or completion recording fails, the engine rolls that config set back immediately from the same journal before continuing with independent sets. A journal-intent write failure is fatal before the first target mutation.

Journal intents have explicit `pending`, `committed`, and `rolled_back` states. Before any later restore-capable mutation, the engine scans for `pending` intents and attempts idempotent rollback from the already-recorded backups/actions. If recovery cannot complete, the new run fails with `recovery_required` before any new config mutation. This covers process death at any point after intent persistence. Revert records both concrete target actions and the source/target generation path, source-generation fingerprint, capture-time module revision, and restore-time module revision.

### 8. Legacy behavior remains explicit, usable, and separate

Bundle/manifest v1 and schema-v1 modules retain the current inline restore path. The new engine reports those config payloads as `legacy_unverified`; it does not manufacture source versions or generations. Installation proceeds, and restore remains available through the existing explicit consent flow with the same conflict, backup, journal, and revert protections. No additional expert flag is required.

A manifest-v2 bundle may contain both generation-aware config captures and schema-v1 flat payloads. Flat restore entries are permitted only for the explicitly identified schema-v1 payloads, which remain `legacy_unverified`; they can never supply missing data or act as fallback for an invalid generation-aware capture.

A v2 bundle with invalid generation provenance never falls back to this legacy path. That distinction prevents malformed new data from bypassing compatibility checks.

### 9. Engine output is detailed; GUI language is distilled

JSON envelopes gain `configResolutions[]` and a config-level summary. Existing `restoreItems[]` remain concrete action results and gain optional capture/generation fields for v2 restores. JSONL adds config-resolution and config-migration progress without changing the existing app `items[]` contract.

The engine exposes stable machine states; the GUI maps them to four default labels:

- `direct` -> **Compatible**
- `migrate` -> **Will be upgraded**
- `unknown` or `legacy_unverified` -> **Compatibility unknown**
- `incompatible` -> **Not supported**

Advanced details may show source/target versions, generations, migration path, module revisions, and reasons. The GUI does not recompute any of them.

## Risks / Trade-offs

- **[Vendor versions are irregular]** -> Preserve raw values, use explicit detector extraction, and resolve zero/multiple matches as unknown rather than guessing.
- **[Module schema becomes substantially richer]** -> Keep v1 valid, make v2 optional, reuse existing capture/restore primitives, and provide catalog validation plus representative examples.
- **[A bad migration could damage settings]** -> Restrict operations, transform only staging, validate every edge and final output, back up before commit, journal intent before mutation, and roll back the config set on failure.
- **[Current catalogs can forget or redefine old generations]** -> Record source-generation fingerprints, require explicit acceptance of historical fingerprints, validate released history in CI, and surface missing/changed knowledge without fallback.
- **[Editable hashes do not prove authenticity]** -> Describe hashes only as integrity checks; signing remains a separate feature.
- **[Released legacy engines may still process v2 bundles]** -> They may process application declarations and explicitly represented legacy lanes, but they cannot execute generation-aware payloads because those payloads exist only in `configCaptures[]` and have no flat restore path. New engines enforce strict v1/v2 dispatch and continue to restore legacy bundles.
- **[Per-set transactions may overlap host paths]** -> Preflight rejects exact and parent/child target collisions.
- **[Post-install resolution can differ from preview]** -> Pin the catalog for each run, re-detect immediately before restore, and emit the final resolution before any config mutation.
- **[A process can die during commit]** -> Persist pending intent after backups and before mutation, recover pending intents before future mutation, and block with `recovery_required` when rollback cannot complete.

## Migration Plan

1. Add schema-v2 module types, canonical module/generation hashing, released-generation history validation, strict catalog validation, and the schema-v1 adapter.
2. Fix wildcard instance expansion and full relative-path preservation before enabling generation capture.
3. Add instance detection, version normalization, generation matching, resolution planning, and dry-run/envelope reporting.
4. Add manifest/bundle v2 capture with payload manifests, module snapshots, and explicit v1/v2 loading dispatch.
5. Add the staged migration operation registry, graph planner, validators, and config-set transaction/journal integration.
6. Add explicit target selection, side-by-side/target-collision handling, events, capabilities, and GUI contract fields.
7. Convert three representative modules: one stable-layout module, one versioned-path module, and one module requiring a forward JSON/INI migration. Existing modules remain v1.
8. Release the engine before the GUI consumer. Rollback disables v2 capture while retaining v2 read support; already-created v2 bundles are never rewritten to v1.

## Open Questions

None. Reverse migration, additional detector types, extra transformation formats, and bundle signing are intentionally deferred rather than left ambiguous.
