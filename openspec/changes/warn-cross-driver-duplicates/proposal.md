## Why

A manifest can intentionally or accidentally route the same physical application through two package managers. Endstate must preserve both explicit declarations, but it should surface the ownership risk before users assume the two entries are independent.

## What Changes

- Add a conservative, non-blocking `possible_duplicate` warning to `plan`, `apply`, and `verify` when package entries resolved to different per-package drivers have equal non-empty explicit display names after trimming and case folding.
- Preserve every declared action and its authoritative driver; warnings never deduplicate, reroute, block, change status, or change summary counts.
- Keep matching exact and deterministic: no fuzzy names, ref aliases, punctuation normalization, catalog mapping, or automatic ownership selection.
- Add `warnings` to plan and verify result payloads using the existing additive `CommandWarning` shape already used by capture and apply.
- Document GUI handling as advisory: render the warning while keeping both entries visible and actionable.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `multi-driver-package-management`: extend conservative cross-driver identity handling from capture into plan, apply, and verify without changing explicit driver authority.

## Impact

The change affects command-layer warning generation, plan/apply/verify JSON payloads, hermetic command tests, and the CLI/GUI integration contracts. It adds no dependency, flag, manifest field, event type, or schema-version bump.
