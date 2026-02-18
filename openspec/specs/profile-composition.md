# Spec: Profile Composition — Includes with Profile Name Resolution and Exclusions

## Overview

Profiles can include other profiles by name, enabling machine-specific customization on top of shared captured bundles. A laptop profile can inherit a desktop capture, exclude irrelevant apps, suppress specific config restores, and add machine-specific apps — all in a single manifest.

This follows the Kustomize base+overlay pattern: a broadly-scoped captured base is refined per-target via explicit, inspectable overrides.

## Motivation

Capture produces full machine snapshots. Target machines need customization:
- Exclude hardware-specific apps (audio interfaces, drive tools)
- Install app but skip its config (different monitor layout, different keybindings)
- Add apps the source machine didn't have

Without composition, users must either maintain separate captures per machine or manually install differences.

## Schema Changes

### New optional fields on manifest

| Field | Type | Description |
|-------|------|-------------|
| `exclude` | `string[]` | Winget IDs to remove from merged app list |
| `excludeConfigs` | `string[]` | Config module IDs to suppress from restore |

### Extended `includes` semantics

Include entries are discriminated by extension:
- **Has extension** (`.jsonc`, `.json`, `.yaml`, `.yml`, `.zip`) → file path (existing behavior)
- **No extension** → profile name, resolved via `Resolve-ProfilePath` in `Documents\Endstate\Profiles\`

## Behavior

### Include Resolution

When processing `includes`, for each entry:

1. If entry has a file extension → resolve as file path relative to manifest directory (existing behavior, unchanged)
2. If entry has no extension → resolve as profile name:
   a. Call `Resolve-ProfilePath(entry, ProfilesDir)` → zip / folder / bare
   b. If zip → extract via `Expand-ProfileBundle`, read `manifest.jsonc` from extracted dir
   c. If folder → read `manifest.jsonc` from folder
   d. If bare → read the `.jsonc` file directly
   e. If not found → error: "Included profile not found: {name}"
3. Merge included manifest's arrays (`apps`, `restore`, `verify`) additively into parent (existing merge logic)
4. Included manifest's own `includes` are resolved transitively (existing behavior)

### Exclude Processing

After all includes are merged:

1. For each entry in `exclude`:
   - Remove any app from merged `apps` where `app.refs.windows` matches the exclude entry (exact match)
   - Remove any associated config module payloads for the excluded app
2. `exclude` matches against winget ID (`refs.windows`), not app `id`

### ExcludeConfigs Processing

After all includes are merged:

1. For each entry in `excludeConfigs`:
   - Suppress config module restore for the named module ID
   - The app itself is still installed — only config restoration is skipped
2. `excludeConfigs` matches against config module ID (e.g., `"powertoys"`, `"windows-terminal"`)

### Exclude Implies ExcludeConfigs

If an app is in `exclude`, its config is also suppressed. There is no need to list it in both.

### Zip Temp Directory Lifecycle

When an included profile resolves to a zip:
- Extracted temp directory MUST remain accessible until the apply/verify run completes
- Config payloads from the extracted `configs/` directory are available for `--enable-restore`
- Cleanup of all temp directories occurs after run completion (success or failure)

### Composition Depth Rule

- **Transitive includes:** An included profile's own `includes` ARE resolved recursively (existing behavior)
- **Non-transitive exclusions:** An included profile's `exclude` and `excludeConfigs` are NOT inherited. Only the root profile's exclusions apply
- This ensures the final plan is always determinable from the root manifest alone

## Example

### Main machine capture
```
Documents/Endstate/Profiles/
  hugo-desktop.zip          # captured bundle (72 apps + configs)
```

### Laptop profile
```
Documents/Endstate/Profiles/
  hugo-desktop.zip          # copied from main machine
  win-laptop.jsonc          # authored on laptop
```

```jsonc
// win-laptop.jsonc
{
  "version": 1,
  "name": "win-laptop",
  "includes": [
    "hugo-desktop"
  ],
  "exclude": [
    "FocusriteAudioEngineeringLtd.FocusriteControl",
    "Seagate.SeaTools",
    "Apple.AppleMobileDeviceSupport",
    "Apple.AppleSoftwareUpdate"
  ],
  "excludeConfigs": [
    "powertoys",
    "windows-terminal"
  ],
  "apps": [
    { "id": "lightroom", "refs": { "windows": "Adobe.Lightroom" } }
  ]
}
```

```powershell
# Single command handles everything
endstate apply --profile "win-laptop"

# Verify includes base + local apps minus exclusions
endstate verify --profile "win-laptop"
```

### Mixed includes (file path + profile name)
```jsonc
{
  "version": 1,
  "includes": [
    "hugo-desktop",              // profile name → resolves to .zip
    "./laptop-dev-extras.jsonc"  // relative file path → existing behavior
  ],
  "apps": []
}
```

## Invariants

### INV-COMPOSE-1: Extension-based discrimination is deterministic
An include entry with an extension is ALWAYS a file path. An include entry without an extension is ALWAYS a profile name. No fallback between the two.

### INV-COMPOSE-2: Composition is single-depth for exclusions
Only the root profile's `exclude` and `excludeConfigs` are applied. Included profiles' exclusions are ignored. This prevents cascading exclusion interactions.

### INV-COMPOSE-3: Exclude matches winget ID exactly
`exclude` entries match against `app.refs.windows` using exact string match. No wildcards, no partial matching.

### INV-COMPOSE-4: App list merge is additive (no dedup)
Duplicate app IDs across includes and root are preserved. The engine's idempotent apply handles duplicates gracefully (second install skips as "already installed"). Dedup may be added in a future version as an optimization.

### INV-COMPOSE-5: Included zip configs survive until run completion
Temp directories from zip extraction MUST NOT be cleaned up until the full apply/verify pipeline completes, including `--enable-restore` config application.

### INV-COMPOSE-6: Profile name resolution uses canonical Profiles directory
Profile names in includes resolve against `Documents\Endstate\Profiles\` using existing `Resolve-ProfilePath` (zip → folder → bare). No other directories are searched.

### INV-COMPOSE-7: Backward compatibility preserved
Existing manifests with file-path-only includes continue to work unchanged. The `exclude` and `excludeConfigs` fields default to empty arrays via `Normalize-Manifest`.

## Implementation Scope

### Engine changes (engine/manifest.ps1)
- `Resolve-ManifestIncludes`: Add extension check, route extensionless entries to `Resolve-ProfilePath`
- `Resolve-ManifestIncludes`: For zip results, call `Expand-ProfileBundle` and track temp dirs
- `Normalize-Manifest`: Add `exclude` and `excludeConfigs` to default empty arrays
- `Read-Manifest`: After include merge, apply `exclude` filter on apps list
- `Read-Manifest`: After include merge, apply `excludeConfigs` to suppress config modules

### Engine changes (engine/bundle.ps1)
- No changes to existing functions
- Temp dir lifecycle managed by caller (manifest.ps1 or apply.ps1)

### Engine changes (engine/apply.ps1)
- Ensure temp dir cleanup occurs in finally block after full run

### No GUI changes required
The GUI calls `apply --profile` and receives a flat result. Composition is invisible to the GUI.

### No schema version bump required
`exclude` and `excludeConfigs` are additive optional fields. Existing manifests without them are valid. This is a non-breaking change per the versioning contract.

## Non-Goals

- No dedup of app entries across includes (deferred — idempotent apply handles it)
- No per-path config exclusion (Tier 3 — deferred)
- No deep inheritance chains (composition is the pattern)
- No wildcard matching in `exclude` (exact winget ID only)
- No GUI awareness of composition (GUI sees flat results)
- No `exclude` inheritance from included profiles
