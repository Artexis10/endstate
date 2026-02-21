# Restore Safety & Per-App Config Selection Contract

**Status:** Draft
**Version:** 1.0
**Last Updated:** 2026-02-21

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

### onConflict Field

Each restore entry supports an `onConflict` field:

| Value | Behavior | Default |
|-------|----------|---------|
| `skip` | Only restore if target does not exist | **Yes (default)** |
| `backup-and-overwrite` | Create backup, then overwrite | No |
| `overwrite` | Overwrite without backup (destructive) | No |

When `onConflict` is not specified, the engine treats it as `skip`.

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
- No flag exists for `overwrite` without backup. Backup is always created when overwriting.

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

Revert uses the existing journal-based revert system (see config-portability-contract.md). No changes needed — just surface it prominently in the GUI.

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

### INV-RESTORE-SAFETY-5: Advanced Never Required

All core restore functionality works without entering advanced mode. Advanced mode reveals detail and control — it never unlocks behavior that should be default.

---

## Module Integration

### How Modules Feed the GUI

The GUI reads module definitions to populate the per-app advanced view:

1. Module `restore` entries → list of config paths per app
2. Module `capture.files` entries → determines what's in the bundle
3. Engine pre-scans targets → reports which exist on the machine
4. GUI renders three groups: NEW, EXISTS, MISSING FROM BUNDLE

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
