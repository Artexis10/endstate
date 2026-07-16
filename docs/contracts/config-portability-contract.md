# Endstate Config Portability Contract (manifest v1 and v2)

**Status:** Stable
**Audience:** Engine, GUI, future automation

**Purpose**
Define the non-negotiable contract for configuration export, restore, and revert in Endstate.
Any engine or GUI change **must preserve the guarantees defined here**.

---

## 1. Core principle

For manifest-v1 bundles and schema-v1 modules, Endstate configuration portability is defined by **flat restore entries**.

A restore entry maps a **portable source path** to a **system target path**.

Manifest-v2 bundles add generation-aware `configCaptures[]`. Each record represents one independently evolving config set captured from one detected application/config instance. The engine resolves that record to trusted current-catalog restore actions before mutation. A generation-aware payload never receives a flat restore entry or legacy fallback path.

Both lanes ultimately use engine-owned restore primitives, backup, journaling, and revert. Compatibility decisions belong to the engine, not the GUI or bundle.

The bundle's source instance/version, generation ID, generation fingerprint, capture module revision, and payload bytes are immutable source facts. The current trusted catalog owns target discovery and migration knowledge. A module-revision difference alone is not incompatibility; if the same released generation ID now has a different fingerprint, the current generation must explicitly accept that historical fingerprint or resolution is `unknown` with `source_generation_definition_changed`.

Modules without `moduleSchemaVersion` remain schema v1 and unversioned. Schema-v2 modules declare independently evolving config sets and stable generations identified by `<moduleId>/<configSetId>/<generationId>`. App/version evidence selects exactly one generation for each set; zero or multiple matches remain unknown. A generation's positive `order` establishes forward direction only and never creates compatibility or a migration edge.

---

## 2. Restore entry contract

A restore entry represents the following logical mapping:

* type: `copy`
* source: `./configs/app/file-or-folder`
* target: `C:/System/Path/file-or-folder`
* backup: `true`
* exclude: `["**\\Logs\\**", "**\\Temp\\**"]` (optional)

**Guarantees**

* `source` is always a portable, relative path
* `target` is always a system path
* `backup: true` means:

  * If the target exists before restore, a backup **must** be created
  * If the target does not exist, no backup is created

### Exclude patterns (directory copy only)

The optional `exclude` field allows skipping files/folders during directory copy restore operations.

**When to use**

Apps like PowerToys may have locked log files or runtime caches that prevent restore from succeeding. Exclude patterns let you skip these paths silently.

**Example: PowerToys**

```json
{
  "type": "copy",
  "source": "./configs/powertoys",
  "target": "%LOCALAPPDATA%\\Microsoft\\PowerToys",
  "backup": true,
  "exclude": [
    "**\\Logs\\**",
    "**\\Temp\\**",
    "**\\Cache\\**"
  ]
}
```

**Semantics**

* `exclude` is an array of glob-like patterns
* Patterns are matched against the relative path inside the source directory
* `**` matches any path segment(s)
* Matching files/folders are skipped silently (no errors)
* If a skipped file is locked, restore still succeeds
* Non-excluded files that fail to copy cause restore to fail (existing behavior)
* Excluded paths do not appear in the journal as failures

**Common excludes**

* `**\\Logs\\**` - Application log directories
* `**\\Cache\\**` - Cache directories
* `**\\Temp\\**` - Temporary files
* `**\\*.lock` - Lock files

---

## 3. Export / restore symmetry

Export and restore are strict inverses for schema-v1 `copy` entries.

**Export (export-config)**

* Reads restore entries
* Copies from system target → portable source
* Writes results into an export snapshot

**Restore (restore)**

* Copies from portable source → system target
* May overwrite existing system state
* May create new files or folders

This symmetry must always hold.

For schema-v2 config captures, the captured payload is immutable input. Direct restore may preserve the same generation; forward migration transforms a staging copy through an explicit current-catalog migration path. Revert restores concrete pre-run target state from the journal; it does not reverse the migration graph or alter the captured payload.

---

## 4. Model B source resolution (critical)

When resolving a flat schema-v1 restore entry source, the engine applies the following rule.

### When `-Export <ExportRoot>` is provided

1. Resolve source from `<ExportRoot>/<sourceRelative>`
2. If not found, fallback to `<ManifestDir>/<sourceRelative>`

### When `-Export` is NOT provided

Resolve source from `<ManifestDir>/<sourceRelative>`

**Rationale**

* Allows clean, reusable manifests
* GUI never rewrites manifests
* Enables restoring from arbitrary export snapshots

This rule applies identically to:

* restore
* validate-export

Generation-aware payloads resolve only from their declared `configs/<captureId>/` payload root and verified payload manifest. Model B fallback never supplies missing data for a `configCaptures[]` record.

---

## 5. Restore journaling (required)

Every **non-dry-run** restore must write a journal file.

**Journal path**

* `./logs/restore-journal-<RunId>.json`

**Journal guarantees**

* Written only for non-dry-run restores
* Written even if some restore steps fail
* Deterministic and machine-readable

Each journal records:

* runId
* timestamp
* manifestPath
* manifestDir
* exportRoot (nullable)
* entries

Each entry records:

* resolvedSourcePath
* targetPath
* targetExistedBefore
* backupRequested
* backupCreated
* backupPath (nullable)
* action (restored / skipped_up_to_date / skipped_missing_source / failed)
* error (nullable)

### Generation-aware journal intents

Each selected schema-v2 config set is a separate transaction. After staging and validation, the engine:

1. Creates every required backup.
2. Atomically persists a `pending` journal intent containing the capture ID, target instance ID, source/target generations, source-generation fingerprint, ordered migration path, capture-time and restore-time module revisions, concrete actions, and backup locations.
3. Commits the resolved actions and validates the target generation.
4. Atomically marks the intent `committed`.

A journal-intent failure occurs before target mutation. A commit, validation, or completion-record failure after mutation triggers immediate rollback from the same intent. Successful rollback marks the intent `rolled_back`; incomplete rollback blocks all later config-set mutation in that run.

Before any later restore-capable mutation, the engine scans for `pending` intents and attempts idempotent rollback. If recovery cannot prove complete restoration, the new run fails with `recovery_required` before new config mutation.

---

## 6. Revert semantics (journal-based)

Revert is defined as **Undo the last restore** and is journal-driven.

Revert processes journal entries in reverse order:

1. If a backup exists → restore backup to target
2. Else if the target did not exist before restore and was restored → delete the created target
3. Else → no operation

**Explicit non-goals**

* Do not revert skipped_up_to_date entries
* Do not invent backups
* Do not guess prior state

If no suitable journal exists, revert must clearly report that nothing can be reverted.

For generation-aware entries, revert uses the recorded concrete actions and backups. Generation lineage is audit data; revert never invents or executes a reverse migration.

---

## 7. CLI contract (stable)

The GUI and automation may rely on the following behaviors.

**Export**
`export-config -Manifest <path> [-Export <snapshotRoot>] [-DryRun]`

**Validate**
`validate-export -Manifest <path> [-Export <snapshotRoot>]`

**Restore**
`restore -Manifest <path> [-Export <snapshotRoot>] -EnableRestore [-DryRun] [--restore-target <captureId>=<targetInstanceId>]`

**Revert**
`revert [...]`

Behavior must conform to this contract regardless of CLI help formatting.

`--restore-target` is repeatable on apply, standalone restore, and rebuild. Module-level `--restore-filter` runs first. Malformed mappings, duplicate mappings for one capture ID, and unknown capture IDs are input errors before installation or configuration mutation. A well-formed mapped target that is absent or incompatible after final post-install detection skips only that config set; successful installation remains intact.

---

## 8. Canonical directory layout

**User-authored (GUI-managed)**

* `%USERPROFILE%\\Documents\\Endstate\\Modules`
* `%USERPROFILE%\\Documents\\Endstate\\Bundles`
* `%USERPROFILE%\\Documents\\Endstate\\Profiles`

**Generated / runtime**

* `./exports/<RunId>/...`
* `./logs/restore-journal-<RunId>.json`

**Generation-aware bundle layout**

* `./configs/<captureId>/...` — hierarchy-preserving config-set payload
* `./provenance/modules/...` — canonical, non-executable capture-time module snapshots
* `manifest.jsonc` version `2` with `configCaptures[]`
* `metadata.json` schema `2.0`

---

## 9. Stability rule

Any change that alters:

* source resolution
* restore behavior
* journaling
* revert semantics

**must update this document in the same commit.**

This document is the **single source of truth** for GUI and engine alignment.

---

## 10. Definition of done

Config portability is GUI-grade when:

* Restore consumes export snapshots without manifest edits
* Restore always journals state changes
* Revert reliably undoes restore effects
* The GUI never needs file-level compatibility or migration knowledge
* Generation-aware restores resolve every config set before mutation
* Side-by-side targets are never resolved by a hidden newest-version rule
* Captured source provenance remains unchanged while the pinned current catalog owns target rules

---

End of contract.
