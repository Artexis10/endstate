## Why

The GUI needs a trustworthy, profile-specific inventory of apps and captured settings without re-evaluating the current machine or inferring ownership from catalog metadata. A dedicated engine inspection contract keeps the CLI authoritative and lets compatible GUI versions render exact saved-profile semantics.

## What Changes

- Add the read-only `endstate profile inspect <manifest-path> --json` capability for extracted manifests only.
- Define deterministic profile-owned app and settings inventories, including canonical ownership normalization, composition/exclude handling, grouping, row identifiers, labels, warnings, counts, and the schema-1.x JSON result envelope.
- Define capability negotiation through `features.profileInspection` so stale engines produce an update-required state rather than GUI-side ownership inference.
- Update the profile, CLI JSON, and GUI integration contracts for this additive cross-repository boundary.

## Capabilities

### New Capabilities

- `profile-inspection`: Read an extracted saved profile and return a deterministic, non-mutating inventory of its apps and captured settings.

### Modified Capabilities

- None.

## Impact

- Affects the engine profile command surface, restricted manifest/include and capture-provenance readers, the pure trusted-catalog loader, JSON envelope output, capabilities response, and GUI profile-contents presentation.
- Adds no new dependencies and preserves schema major version 1.
