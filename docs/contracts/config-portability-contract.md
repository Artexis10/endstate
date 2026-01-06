# Endstate Config Portability Contract (v1)

**Status:** Stable
**Audience:** Engine, GUI, future automation

**Purpose**
Define the non-negotiable contract for configuration export, restore, and revert in Endstate.
Any engine or GUI change **must preserve the guarantees defined here**.

---

## 1. Core principle

Endstate configuration portability is defined entirely by **restore entries**.

A restore entry maps a **portable source path** to a **system target path**.

All higher-level features (export, restore, revert, GUI flows, recipes, bundles) are built on top of this primitive.

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

Export and restore are strict inverses for `copy` entries.

**Export (export-config)**

* Reads restore entries
* Copies from system target → portable source
* Writes results into an export snapshot

**Restore (restore)**

* Copies from portable source → system target
* May overwrite existing system state
* May create new files or folders

This symmetry must always hold.

---

## 4. Model B source resolution (critical)

When resolving a restore entry source, the engine applies the following rule.

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

---

## 7. CLI contract (stable)

The GUI and automation may rely on the following behaviors.

**Export**
`export-config -Manifest <path> [-Export <snapshotRoot>] [-DryRun]`

**Validate**
`validate-export -Manifest <path> [-Export <snapshotRoot>]`

**Restore**
`restore -Manifest <path> [-Export <snapshotRoot>] -EnableRestore [-DryRun]`

**Revert**
`revert [...]`

Behavior must conform to this contract regardless of CLI help formatting.

---

## 8. Canonical directory layout

**User-authored (GUI-managed)**

* `%USERPROFILE%\\Documents\\Endstate\\Recipes`
* `%USERPROFILE%\\Documents\\Endstate\\Bundles`
* `%USERPROFILE%\\Documents\\Endstate\\Profiles`

**Generated / runtime**

* `./exports/<RunId>/...`
* `./logs/restore-journal-<RunId>.json`

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

## 10. Definition of done (v1)

Config portability is GUI-grade when:

* Restore consumes export snapshots without manifest edits
* Restore always journals state changes
* Revert reliably undoes restore effects
* The GUI never needs file-level knowledge

---

End of contract.
