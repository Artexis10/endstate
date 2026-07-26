## Why

Capture produces full machine snapshots, but target machines need customization. A laptop restored from a desktop capture wants to skip hardware-specific apps (audio interfaces, drive tools), install an app but skip its config (different monitor layout, different keybindings), and add apps the source machine never had.

Without composition, users must either maintain a separate capture per machine or hand-apply the differences after every restore.

## What Changes

Profiles can include other profiles by name and refine them, following the Kustomize base+overlay pattern — a broadly-scoped captured base refined per-target via explicit, inspectable overrides.

- **Extensionless includes resolve as profile names.** `includes` entries are discriminated by extension: an entry with one (`.jsonc`, `.json`, `.yaml`, `.yml`, `.zip`) stays a file path; an entry without one resolves through `Resolve-ProfilePath` against `Documents\Endstate\Profiles\` (zip, folder, bare precedence).
- **New `exclude` field** removes apps from the merged list by exact `refs.windows` match.
- **New `excludeConfigs` field** suppresses config module restore while still installing the app.
- **Exclude implies config suppression** — an excluded app's config is dropped without listing it twice.
- **Exclusions are single-depth.** Only the root profile's `exclude` / `excludeConfigs` apply, so the final plan is always determinable from the root manifest alone.
- **Included zip bundles are extracted and tracked**, with temp directories surviving until the run completes so `--enable-restore` can read their `configs/` payloads.

## Impact

- Affected specs: `profile-composition`
- Affected code: `engine/manifest.ps1` (`Resolve-ManifestIncludes`, `Normalize-Manifest`, `Read-Manifest`), `engine/apply.ps1` (temp dir cleanup in `finally`)
- **Non-breaking.** `exclude` and `excludeConfigs` are additive optional fields defaulted to empty arrays by `Normalize-Manifest`; existing file-path-only manifests resolve exactly as before. No schema version bump required.
- No GUI changes — the GUI calls `apply --profile` and receives a flat result, so composition is invisible to it.

## Non-Goals

- No dedup of app entries across includes (idempotent apply already skips duplicates)
- No per-path config exclusion
- No wildcard matching in `exclude` — exact winget ID only
- No inheritance of `exclude` from included profiles
