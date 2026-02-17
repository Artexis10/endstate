# Spec: Capture Bundle — Zip-Based Profile Packaging

## Overview

Capture produces a single portable zip artifact containing the app manifest, config module payloads, and metadata. This zip is the unit of portability for machine-to-machine transfer.

## Behavior

### Capture Output

- `endstate capture --profile "Name"` produces `Documents\Endstate\Profiles\Name.zip`
- Zip contains: `manifest.jsonc`, `metadata.json`, and optionally `configs/<module-id>/<files>`
- Config bundling is automatic — no flag needed
- If no config modules match, zip contains manifest + metadata only (install-only profile)

### Config Module Matching

- After app scan, engine checks each captured app against config module catalog
- Match via `matches.winget` array against captured app's `refs.windows`
- Matched modules' `capture.files` are copied into zip under `configs/<module-id>/`
- Files matching `capture.excludeGlobs` are skipped
- Files listed in `sensitive.files` are NEVER included

### Profile Discovery

Three formats recognized in `Documents\Endstate\Profiles\`:
1. `<name>.zip` — zip bundle (preferred)
2. `<name>\manifest.jsonc` — loose folder
3. `<name>.jsonc` — bare manifest (legacy)

Resolution order for `--profile "Name"`: zip → folder → bare manifest. First match wins.

### Apply from Zip

- `endstate apply --profile "Name.zip"` extracts to temp dir, reads manifest, installs apps
- Config restore from zip requires `--enable-restore`
- Temp extraction cleaned up after apply completes

### JSON Output

Capture success response includes:
- `outputPath` — path to zip file
- `outputFormat` — `"zip"`
- `configsIncluded` — array of module IDs bundled
- `configsSkipped` — array of module IDs skipped
- `configsCaptureErrors` — array of capture error descriptions

## Invariants

### INV-BUNDLE-1: Zip is self-contained
The zip MUST contain everything needed to restore on another machine. No external file references.

### INV-BUNDLE-2: Sensitive files never bundled
Files listed in `module.sensitive.files` MUST NOT appear in the zip. Enforced at capture time.

### INV-BUNDLE-3: Config capture failures don't block app capture
Missing or inaccessible config files are reported in `captureWarnings` and `metadata.json`. Manifest is always written.

### INV-BUNDLE-4: Install-only is always valid
A zip with only `manifest.jsonc` + `metadata.json` (no configs) is a valid profile. No warnings, no errors.

### INV-BUNDLE-5: Zip is inspectable
Users can unzip and review/edit contents. Unzipped folder is itself a valid loose-folder profile.

## Metadata Schema

```json
{
  "schemaVersion": "1.0",
  "capturedAt": "<ISO 8601>",
  "machineName": "<COMPUTERNAME>",
  "endstateVersion": "<version>",
  "configModulesIncluded": [],
  "configModulesSkipped": [],
  "captureWarnings": []
}
```

## Non-Goals

- No `--zip` / `--no-zip` flags
- No encryption or signing
- No remote transfer protocol
- No GUI changes
- No changes to `restore` or `revert` commands
