# Restore Safety & Per-App Config Selection Contract

**Status:** Draft
**Version:** 1.0
**Last Updated:** 2026-07-16

## Purpose

This document defines the safety contract for configuration restore operations. It governs how the GUI presents restore choices, handles conflicts with existing files, and ensures reversibility.

This contract exists because config restore is the one area where Endstate can cause real damage to a working machine.

---

## Core Safety Rule

**Existing files are never overwritten without explicit user awareness.**

If a restore target already exists on the machine, the default behavior is to skip it. Overwriting requires either:
- Advanced mode review with per-path selection, or
- Explicit "overwrite all" confirmation in the default flow

---

## Engine Contract

### Compatibility Resolution Precedes Mutation

For every captured config set, the engine resolves compatibility before any target write as exactly one of:

| Resolution | Meaning |
|------------|---------|
| `direct` | Source and selected target use the same config generation |
| `migrate` | The pinned current catalog provides one explicit forward migration path |
| `incompatible` | The target is known but no supported direction/path exists |
| `unknown` | Required source, catalog, version, generation, or target knowledge is absent or ambiguous |
| `legacy_unverified` | Schema-v1 payload has no trustworthy generation provenance |

Application versions are evidence used to select config generations; they are not themselves compatibility. Schema-v2 modules may contain independently evolving config sets, so one application can restore `preferences/g2` while `presets/g1` is skipped.

Same-generation transfer is direct. Different generations require one explicit, uniquely resolvable forward migration path in the same config set. Generation `order` proves direction but never creates an edge; a lower-order target is an unsupported downgrade. No path, ambiguous path, changed/unaccepted source fingerprint, or missing current-catalog knowledge remains incompatible/unknown rather than guessed.

The engine never chooses a newest side-by-side instance. It auto-selects only one viable target or one unique exact-version target. Otherwise it reports `ambiguous_target_instance` until the caller supplies `--restore-target <captureId>=<targetInstanceId>`. Module-level `--restore-filter` applies first. Invalid mapping syntax, duplicate capture mappings, and unknown/non-targetable capture IDs return `INVALID_RESTORE_TARGET` with engine-authored message and remediation before installation or config mutation; a mapped target absent/incompatible after final post-install detection skips only that set.

Source provenance comes from the bundle and remains immutable. Target discovery, current generation definitions, and migration edges come only from the trusted catalog snapshot pinned for the run. Bundle-supplied module snapshots are inspectable but never executable or authoritative.

Every result preserves a portable, non-secret `sourceInstance` and a non-null `targetCandidates[]` containing portable target identity and version evidence. Host-local target roots and locators remain internal engine data. The engine authors `label`, `message`, nullable `remediation`, and all technical detail; GUI consumers render those values verbatim and never reconstruct them from application versions, candidates, module rules, or bundle data.

Stable per-set reasons include compatibility causes plus `restore_filtered`, `restore_not_enabled`, `target_detection_failed`, `staging_validation_failed`, `backup_failed`, `journal_intent_failed`, `commit_failed`, `target_validation_failed`, `journal_completion_failed`, and `already_up_to_date`. A missing reason or remediation is serialized as `null`.

Preflight rejects selected config sets whose concrete target paths are equal or overlap by parent/child containment, and rejects multiple captured sets competing for one target instance/config set. Neither colliding set mutates its target.

### onConflict Field

Each restore entry supports an `onConflict` field:

| Value | Behavior | Default |
|-------|----------|---------|
| `skip` | Only restore if target does not exist | **Yes (default)** |
| `backup-and-overwrite` | Create backup, then overwrite | No |
| `overwrite` | Overwrite without backup (destructive) | No |

When `onConflict` is not specified, the engine treats it as `skip`.

### Value-level `registry-set`: backup-and-overwrite (exception to default-skip)

The file-based restore strategies default to **skip**: when a target already
exists it is left untouched unless the action opts into overwrite. The
value-level `registry-set` strategy (Windows OS-settings tier) is an explicit,
documented **exception**: its default is **backup-and-overwrite**.

A `registry-set` action targets exactly one named value under an HKCU key.
Because a single OS-preference value has no merge or partial-update semantics,
"skip an existing value" would make the declared desired state unreachable.
Instead, `registry-set`:

1. Records the prior value — its `REG_*` type and data, or the fact that it was
   absent — to `state/backups/<runID>/` **before** any write. This backup is
   mandatory and unconditional (it is not gated on a `backup: true` flag).
2. Skips the write only when the named value already equals the desired
   type+data (idempotency); in that case no backup is written because nothing
   changes.
3. Otherwise creates the key if missing and writes the desired value.

Reversibility is guaranteed by construction: `revert` restores the exact prior
value, or **deletes** the value when it was absent before the write. The scope
is HKCU-only and Windows-only; non-HKCU targets and unsupported value types are
rejected before any registry access. `registry-set` performs no uninstall,
debloat, service, or scheduled-task changes — it is a reversible single-value
write.

### Restore Result Per Entry

Each restore entry emits a result with:

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | Source path in bundle |
| `target` | string | Target path on system |
| `action` | string | `restored`, `skipped_exists`, `skipped_up_to_date`, `skipped_missing_source`, `failed` |
| `targetExistedBefore` | boolean | Whether target existed before restore |
| `backupCreated` | boolean | Whether a backup was created |
| `backupPath` | string or null | Backup location if created |

### CLI Flags

- `--enable-restore` — required to activate restore (existing, unchanged)
- `--restore-overwrite` — override `onConflict` to `backup-and-overwrite` for all entries
- `--restore-target <captureId>=<targetInstanceId>` — repeatable explicit target selection for side-by-side generation-aware captures
- No flag exists for `overwrite` without backup. Backup is always created when overwriting.

### Generation-Aware Staging and Config-Set Transactions

Before host mutation, the engine verifies the payload manifest, copies one config set to fresh staging, applies only allowlisted forward migration operations inside staging, validates every migration edge, and validates the final target generation. Captured bytes are read-only. The initial operation allowlist is `file-copy`, `file-move`, `file-delete`, `json-set`, `json-delete`, `json-move`, `ini-set`, `ini-delete`, and `ini-move`; validation is limited to `file-exists`, `json-parse`, `json-path-exists`, `ini-parse`, and `ini-key-exists`. Unsupported formats are reported; modules and bundles cannot run shell, PowerShell, batch, executables, dynamic plugins, generic regex replacements, or host-absolute migration writes.

After staging succeeds, each config set commits transactionally:

1. Create all required backups.
2. Atomically persist a `pending` journal intent with concrete actions and backup locations.
3. Commit the resolved restore actions.
4. Validate the committed target generation.
5. Atomically mark the intent `committed`.

Staging validation uses `staging_validation_failed`; backup failure uses `backup_failed`; intent persistence failure occurs before mutation and uses `journal_intent_failed`. Commit, final target validation, or completion-record failure uses `commit_failed`, `target_validation_failed`, or `journal_completion_failed` respectively and triggers immediate rollback of that config set after mutation. A verified rollback allows safe non-overlapping sets to continue. Incomplete rollback stops all later config-set mutation. Before any future restore-capable mutation, pending intents are recovered idempotently; unrecoverable state fails the new run with `recovery_required` before new writes.

If a generation requires its application closed, a running app produces `app_running`; the engine never stops or kills it.

### Config-Set Terminal Status

Envelope status is independent from compatibility resolution and is exactly one of:

| Status | Contract meaning |
|--------|------------------|
| `planned` | Dry-run only: selected set passed compatibility, integrity, preflight, staging, and validation |
| `restored` | Live transaction reached and validated desired state and durably recorded completion |
| `skipped` | No mutation was attempted because non-execution was intentional or safely required, including filtering/consent, unknown/incompatible resolution, absent/incompatible mapped target, `app_running`, or already-up-to-date state |
| `failed` | Selected set failed before any target mutation, so rollback was unnecessary |
| `rolled_back` | Mutation began, failed, and rollback durably restored and verified complete pre-run state |
| `rollback_failed` | Mutation began and complete restoration could not be proven; later config-set mutation is blocked |

For failure statuses, `reason` retains the primary integrity, staging, backup, journal, commit, or validation failure. Rollback outcome is represented by `status`, not by replacing that cause.

When restore-capable input contains config payloads, command data includes `configResolutions[]`, `configResolutionSummary`, and `restoreItems[]`; every result's `targetCandidates[]`, `migrationPath[]`, and `resolvedTargets[]` is also present. These arrays serialize as `[]`, never `null`, when empty. Config-free input omits the config fields entirely. Rebuild's canonical config fields are top-level command data; its nested apply result may mirror them.

---

## GUI Default Flow

### Per-App Restore Toggle

The default flow presents one toggle per app:

```
┌─────────────────────────────────────┐
│ Restore Settings                    │
├─────────────────────────────────────┤
│                                     │
│ ○ Claude Desktop                    │
│   3 config files · 2 already exist  │
│                                     │
│ ● Lightroom Classic                 │
│   26 config paths · 0 already exist │
│                                     │
│ ● VSCodium                          │
│   3 config files · 1 already exists │
│                                     │
│ ● Windsurf                          │
│   3 config files · 1 already exists │
│                                     │
└─────────────────────────────────────┘
```

**Rules for default flow:**

1. Toggle is OFF by default for all apps (unchanged)
2. Each app shows: number of config paths, how many targets already exist
3. When toggled ON with existing targets: brief inline note — "Existing files will be skipped. Use Advanced to choose which to overwrite."
4. Default behavior: `onConflict: skip` — only restores files that don't exist yet
5. No jargon, no file paths, no technical detail

For generation-aware payloads, the engine may expose multiple config sets or side-by-side target choices beneath one app. The GUI renders those engine-provided rows, labels, messages, remediation, technical details, and mappings verbatim; it does not inspect module rules, compare app versions, select generations, infer compatibility, or author replacement copy.

Engine-authored default compatibility labels are locked:

- `direct` → **Compatible**
- `migrate` → **Will be upgraded**
- `unknown` or `legacy_unverified` → **Compatibility unknown**
- `incompatible` → **Not supported**

### Default Restore Behavior

When user enables restore for an app without entering advanced mode:

- Files that don't exist on the machine → restored
- Files that already exist → skipped silently
- Result screen shows what was restored and what was skipped

This is always safe. Worst case: nothing happens.

---

## GUI Advanced Flow (Per-App)

### Entry Point

Each app with restore entries shows an "Advanced" button or expand control. This is progressive disclosure — never required, never prominent.

### Per-Path Selection

```
┌─────────────────────────────────────────────────┐
│ Lightroom Classic — Restore Details             │
├─────────────────────────────────────────────────┤
│                                                 │
│ ☐ Select all                                    │
│                                                 │
│ NEW — safe to restore                           │
│ ☑ Develop Presets                               │
│ ☑ Export Presets                                 │
│ ☑ Filter Presets                                │
│ ☑ Filename Templates                            │
│ ☑ Import Presets                                │
│ ☑ Keyword Sets                                  │
│ ...                                             │
│                                                 │
│ EXISTS — will overwrite (backup created)        │
│ ☐ Preferences ⚠                                │
│   Current: 33.5 KB · Bundle: 31.2 KB           │
│ ☐ CameraRaw Defaults ⚠                         │
│   Current: 1.1 KB · Bundle: 0.9 KB             │
│                                                 │
│ MISSING FROM BUNDLE                             │
│   Color Profiles (not captured)                 │
│   Watermarks (not captured)                     │
│                                                 │
│ [Cancel]  [Restore Selected]                    │
└─────────────────────────────────────────────────┘
```

**Rules for advanced flow:**

1. Paths are grouped by safety:
   - **NEW** (target doesn't exist) — checked by default, green indicator
   - **EXISTS** (target exists) — unchecked by default, yellow indicator with ⚠
   - **MISSING FROM BUNDLE** (source not in bundle) — shown as informational, not selectable
2. Existing files are NEVER checked by default in advanced mode
3. User must explicitly check each existing file they want to overwrite
4. When an existing file is checked, backup is always created (non-negotiable)
5. File sizes shown for existing targets to help user decide
6. No file paths shown unless Advanced Mode (global setting) is enabled

### Advanced Mode (Global) Integration

When the user has global Advanced Mode enabled:
- Show full file paths for source and target
- Show last-modified dates
- Show config format (JSON, XML, INI, etc.)

When global Advanced Mode is disabled:
- Show friendly names only (derived from module displayName and path component)
- No paths, no technical detail

---

## Result Screen

### After Restore

```
┌─────────────────────────────────────────────────┐
│ Restore Complete                                │
├─────────────────────────────────────────────────┤
│                                                 │
│ Lightroom Classic                               │
│   ✔ 12 settings restored                       │
│   — 2 skipped (already exist)                   │
│   — 12 not in bundle                            │
│                                                 │
│ VSCodium                                        │
│   ✔ 2 settings restored                        │
│   ✔ 1 overwritten (backup created)             │
│                                                 │
│ Backups stored at:                              │
│ state/backups/20260221-143052/                  │
│                                                 │
│ [Close]  [Revert All Restores]                  │
└─────────────────────────────────────────────────┘
```

**Rules:**

1. Show per-app summary, not per-file (unless Advanced Mode)
2. Distinguish between "restored" (new), "overwritten" (existed, backup created), and "skipped"
3. Backup location always visible
4. One-click revert prominently available
5. Never show raw file paths in default mode

### Revert

Revert uses the journal-based revert system (see config-portability-contract.md). Generation-aware journals record concrete target actions and generation lineage; revert restores concrete pre-run state and never tries to reverse the migration graph. The GUI surfaces the engine-provided outcome prominently.

Revert button available from:
- Result screen (immediately after restore)
- Profile context menu: "Revert last restore"
- App settings/history (future)

---

## Invariants

### INV-RESTORE-SAFETY-1: No Silent Overwrites

Existing files are never overwritten unless:
- User explicitly selected them in advanced mode, OR
- User explicitly confirmed "overwrite all" in a confirmation dialog

Violation of this invariant is a critical bug.

### INV-RESTORE-SAFETY-2: Backup Before Overwrite

When an existing file is overwritten, a backup is always created. There is no code path that overwrites without backup. The `overwrite` (no backup) mode is engine-internal only and never exposed via GUI.

### INV-RESTORE-SAFETY-3: Revert Always Available

After any non-dry-run restore, the revert action is immediately available. If journal creation fails, the restore itself must fail.

### INV-RESTORE-SAFETY-4: Default is Skip

If no `onConflict` is specified and no user override is active, existing files are skipped. This is the engine default, not just a GUI behavior.

**Exception:** the value-level `registry-set` strategy defaults to backup-and-overwrite (see "Value-level `registry-set`" above). It is reversible by construction — a mandatory pre-write backup of the single prior value, with restore-or-delete on revert — and never alters file-restore behavior.

### INV-RESTORE-SAFETY-5: Advanced Never Required

All core restore functionality works without entering advanced mode. Advanced mode reveals detail and control — it never unlocks behavior that should be default.

### INV-RESTORE-SAFETY-6: No Mutation Before Resolution and Integrity

Every selected generation-aware config set has a final resolution, verified payload, collision-free concrete targets, and validated staging output before its first target mutation. Unknown/incompatible sets are skipped without undoing successful application installation or blocking safe independent sets.

### INV-RESTORE-SAFETY-7: Config-Set Atomicity and Recovery

All backups and a durable pending intent precede target mutation. A partially committed set is rolled back immediately; `rollback_failed` blocks later set mutation. Pending intents are recovered before any later restore-capable mutation.

### INV-RESTORE-SAFETY-8: No Hidden Target or Migration Choice

No declaration order, lexically newest path, highest app version, implicit generation order edge, bundle snapshot, or GUI heuristic may choose a target or migration. Ambiguity remains explicit until the engine can resolve it uniquely or the caller supplies a valid target mapping.

---

## Module Integration

### How Modules Feed the GUI

For schema-v1 lanes, existing module-derived restore metadata may populate the per-app advanced view. For schema-v2 lanes, the GUI consumes engine preflight/envelope data only:

1. Engine `configResolutions[]` → compatibility, portable source instance, non-null target candidates, generations, reasons, presentation, and migration path
2. Engine `restoreItems[]`/preflight → concrete path actions and conflict state
3. Engine target candidates → side-by-side choices keyed by target instance ID
4. GUI renders the supplied data and passes user selections back through documented CLI flags

The GUI never loads a capture-time module snapshot as rules and never recomputes a generation, migration path, target evidence, technical detail, or presentation copy.

Every explicit schema-v1 module lane uses `configSetId: "legacy"` and the deterministic, domain-separated capture ID returned by `bundle.LegacyCaptureID(moduleId)`. Anonymous inline restore actions without a module-lane association remain ordinary restore items; they do not receive fabricated config-resolution rows, instances, versions, or generations.

### Module Display Names

Config paths shown to users derive from:
- Module `displayName` for the app name
- Last path component of `target` for the config name (e.g., "Preferences", "Develop Presets")
- No raw paths in default mode

---

## Terminology

| Term | Meaning |
|------|---------|
| Restore | Copy config from bundle to system |
| Skip | Target exists, not overwritten |
| Overwrite | Target exists, backup created, then replaced |
| Revert | Undo last restore using journal backups |
| New | Target doesn't exist, safe to restore |
| Exists | Target already on system, requires explicit selection to overwrite |

---

## What This Contract Does NOT Cover

- Capture flow (separate concern)
- Export/bundle creation (see config-portability-contract.md)
- Module curation and discovery (engine concern)
- Selective capture (future: capture only specific paths)

---

## Stability Rule

Any change that alters:
- Default `onConflict` behavior
- Overwrite confirmation requirements
- Backup guarantees
- Revert availability

**must update this document in the same commit.**

---

## References

- [Config Portability Contract](config-portability-contract.md) — restore journaling and revert semantics
- [UX Guardrails](../../endstate-gui/docs/ux-guardrails.md) — forbidden behaviors (no silent restore)
- [UX Principles](../../endstate-gui/docs/ux-principles.md) — progressive disclosure, safety defaults
- [UX Engine Contract](../../endstate-gui/docs/ux-engine-contract.md) — GUI ↔ engine alignment
