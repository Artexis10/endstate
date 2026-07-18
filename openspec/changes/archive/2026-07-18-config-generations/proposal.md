## Why

Endstate currently captures and restores application settings without knowing which application version or configuration layout produced them. Applications legitimately change paths and formats over time, so restoring settings blindly can be ineffective or destructive; this needs to become a first-class engine capability before capture bundles see broad use.

## What Changes

- Add configuration generations as the compatibility unit for each independently evolving config set; application versions become evidence used to select a generation rather than the compatibility key itself.
- Extend declarative modules with optional instance discovery, version-to-generation mappings, generation-specific capture/restore definitions, and explicitly directed migration edges.
- Add deterministic engine planning that resolves each captured config set as `direct`, `migrate`, `incompatible`, `unknown`, or `legacy_unverified` before any configuration mutation.
- Add forward-only migration execution through a fixed allowlist of engine-defined operations. Modules and capture bundles remain data and cannot execute shell, PowerShell, batch, or arbitrary executable code.
- Persist immutable source provenance in capture bundles, including the detected app instance/version, config set, generation ID and canonical generation fingerprint, module schema version, and an engine-computed module content hash.
- Use the trusted current module catalog for target discovery and current migration knowledge while preserving the bundle's source facts unchanged. Restore journals record both capture-time and restore-time module identities.
- Keep legacy bundles fully usable: application installation proceeds, configuration compatibility is reported as `legacy_unverified`, and restore remains available through the existing explicit consent, backup, journal, and revert flow.
- Treat side-by-side installed application versions as separate config instances and never silently select a newest or preferred instance.
- Add structured engine output for the GUI to show the distilled outcomes: compatible, will be upgraded, compatibility unknown, or not supported, with technical provenance available through progressive disclosure.
- Make generation-aware bundles safe on unsupported engines: an engine that cannot understand the generation metadata must not blindly apply their configuration payloads.

## Capabilities

### New Capabilities

- `config-generation-modules`: Versioned declarative modules, config sets, instance discovery, application-version-to-generation mappings, stable generation identity, and catalog validation.
- `config-capture-provenance`: Capture-bundle source facts, module identity, payload integrity, hybrid bundle/catalog authority, legacy-bundle behavior, and safe unsupported-engine handling.
- `config-generation-resolution`: Deterministic per-config-set compatibility resolution, target-instance selection, collision detection, and side-by-side instance behavior.
- `config-forward-migration`: Forward-only migration graph planning and safe staged execution through engine-owned declarative operations, validation, journaling, and revert integration.

### Modified Capabilities

- `capture-config-metadata`: Capture output gains per-instance, per-config-set generation provenance and compatibility-relevant metadata for GUI consumers.
- `restore-filter`: Restore selection gains stable captured-config-set and target-instance mapping while preserving module-level filtering.
- `apply-restore-envelope`: Apply/rebuild output gains structured generation resolution and migration results.
- `apply-restore-streaming`: Restore events gain generation, migration, and compatibility reason data without moving decision logic into the GUI.

## Impact

- Module contract and catalog validation in `modules/**/module.jsonc` and `go-engine/internal/modules/`.
- Installed-app/config-instance discovery and version normalization across supported package backends and module-declared discovery sources.
- Capture-bundle layout and metadata in `go-engine/internal/bundle/`, including updates to the legacy capture-bundle and config-portability contracts.
- Rebuild/apply planning, restore staging, verification, journal/revert data, JSON envelopes, and JSONL events in `go-engine/internal/commands/`, `restore/`, `events/`, and related packages.
- GUI integration contracts and the separate Endstate GUI consumer, which remain presentation-only and render engine-provided compatibility decisions.
- Existing modules remain valid and load as unversioned definitions; they are not required to adopt generation metadata immediately.
