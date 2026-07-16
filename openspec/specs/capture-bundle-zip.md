# Spec: Capture Bundle — Zip-Based Profile Packaging

## Overview

Capture produces a single portable zip artifact containing the app manifest, config module payloads, and metadata. This zip is the unit of portability for machine-to-machine transfer.

## Behavior

### Capture Output

- `endstate capture --profile "Name"` produces `Documents\Endstate\Profiles\Name.zip`
- Zip contains `manifest.jsonc`, `metadata.json`, and optional configuration payloads
- A legacy/schema-v1-only capture may use `configs/<module-id>/<files>` with flat restore entries
- A capture containing any schema-v2 config set uses `configs/<captureId>/<complete-relative-hierarchy>`, metadata schema `2.0`, manifest version `2`, and `configCaptures[]`
- Config bundling is automatic — no flag needed
- If no config modules match, zip contains manifest + metadata only (install-only profile)

### Config Module Matching and Instances

- Existing modules without `moduleSchemaVersion` load as schema v1 and keep their current matching and flat capture behavior
- Schema-v2 modules declare engine-owned package/path instance detectors, independently evolving config sets, and generation selectors
- Package detection preserves backend/ref and raw installed version. Path detection expands declared globs and may extract a named raw version
- Raw vendor evidence is retained; numeric dotted normalization is additional evidence, not a replacement
- Zero, one, or many instances may be found. Side-by-side instances remain distinct and capture never chooses the lexically newest or highest version
- Each config set must match exactly one declared generation; zero/multiple matches are reported and are not silently assigned
- Files matching `capture.excludeGlobs` are skipped
- Files listed in `secrets.files` are NEVER included

### Profile Discovery

Three formats recognized in `Documents\Endstate\Profiles\`:
1. `<name>.zip` — zip bundle (preferred)
2. `<name>\manifest.jsonc` — loose folder
3. `<name>.jsonc` — bare manifest (legacy)

Resolution order for `--profile "Name"`: zip → folder → bare manifest. First match wins.

### Apply from Zip

- `endstate rebuild --from "Name.zip"` is the Go engine's zip-consumption path: it extracts to a temp dir, installs apps, restores configs, verifies, then cleans up the temp dir after the full pipeline completes (the historical `apply --profile "Name.zip"` path was retired with the PowerShell engine)
- Config restore runs by default under rebuild and requires explicit `--confirm` consent; `--no-restore` opts out
- New engines explicitly dispatch manifest versions 1 and 2. Version-2 config payloads are resolved through `configCaptures[]`; invalid v2 provenance never falls back to flat restore

### JSON Output

Capture success response includes:
- `outputPath` — path to zip file
- `outputFormat` — `"zip"`
- `configsIncluded` — array of module IDs bundled
- `configsSkipped` — array of module IDs skipped
- `configsCaptureErrors` — array of capture error descriptions
- `bundleSchemaVersion` and `manifestVersion` — artifact compatibility versions
- `configCapture.configSets[]` — per-instance/per-set capture IDs, source version evidence, generations/fingerprints, capture module revisions, file counts, statuses, and reasons

## Invariants

### INV-BUNDLE-1: Zip is self-contained
The zip MUST contain all captured source payload and provenance needed on another machine, with no external payload references. Generation-aware target rules come from the restoring engine's pinned trusted catalog; the bundle never embeds executable target authority.

### INV-BUNDLE-2: Sensitive files never bundled
Files listed in `module.secrets.files` MUST NOT appear in the zip. Enforced at capture time.

### INV-BUNDLE-3: Config capture failures don't block app capture
Missing or inaccessible config files are reported in `captureWarnings` and `metadata.json`. Manifest is always written.

### INV-BUNDLE-4: Install-only is always valid
A zip with only `manifest.jsonc` + `metadata.json` (no configs) is a valid profile. No warnings, no errors.

### INV-BUNDLE-5: Zip is inspectable
Users can unzip and review/edit contents. Unzipped folder is itself a valid loose-folder profile.

### INV-BUNDLE-6: Generation-aware payloads are structurally isolated
A v2 config capture exists only in `configCaptures[]` and has no flat restore entry. An engine that does not understand `configCaptures[]` may still process apps and explicit schema-v1 lanes, but cannot execute generation-aware bytes through the legacy path.

### INV-BUNDLE-7: Source provenance and target authority are separate
The bundle owns immutable source instance/version evidence, source generation/fingerprint, capture-time module revision, and payload bytes. The current trusted catalog pinned by the restoring engine owns target discovery, target generations, and migration edges. A revision difference alone is not incompatibility and never rewrites source facts.

### INV-BUNDLE-8: Payload hierarchy and integrity are complete
Each v2 payload lives under `configs/<captureId>/`, preserves every relative parent directory, rejects duplicate destinations, and records relative path, byte size, and SHA-256 for every entry. Integrity is verified before planning can lead to mutation.

### INV-BUNDLE-9: Source snapshots are inspectable, not executable
Canonical capture-time module snapshots live under `provenance/modules/` and are verified against their recorded content hashes. Restore never executes them or uses them as target/migration authority.

### INV-BUNDLE-10: Mixed bundles keep lanes separate
A manifest-v2 bundle may include explicitly identified schema-v1 flat payloads. Those remain `legacy_unverified` and may use the legacy consent/safety path, but cannot fill missing data or provide fallback for any `configCaptures[]` record.

## Metadata Schema

Schema-v1-only bundle:

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

Generation-aware bundle (additive fields may be present):

```json
{
  "schemaVersion": "2.0",
  "manifestVersion": 2,
  "capturedAt": "<ISO 8601>",
  "machineName": "<COMPUTERNAME>",
  "endstateVersion": "<version>",
  "configModulesIncluded": [],
  "configModulesSkipped": [],
  "captureWarnings": []
}
```

The embedded manifest version `2` (not `metadata.json`) owns the compatibility-relevant records:

```json
{
  "version": 2,
  "apps": [],
  "configCaptures": [
    {
      "captureId": "<stable-capture-id>",
      "moduleId": "apps.example",
      "configSetId": "preferences",
      "sourceInstance": {
        "id": "<stable-instance-id>",
        "detectorId": "installed-package",
        "rawVersion": "27.4",
        "normalizedVersion": "27.4",
        "evidence": {}
      },
      "sourceGeneration": "g1",
      "sourceGenerationFingerprint": "<sha256>",
      "captureModule": {
        "schemaVersion": 2,
        "contentHash": "<sha256>",
        "snapshotPath": "provenance/modules/apps.example.json"
      },
      "payloadRoot": "configs/<stable-capture-id>",
      "payloadManifest": [
        { "relativePath": "settings/preferences.json", "size": 123, "sha256": "<sha256>" }
      ]
    }
  ]
}
```

## Non-Goals

- No `--zip` / `--no-zip` flags
- No encryption or signing
- No remote transfer protocol
- No bundle signing or authenticity claim; SHA-256 fields are integrity checks
- No executable/shell/PowerShell migration code in modules or bundles
- No implicit reverse migration or newest-instance selection
