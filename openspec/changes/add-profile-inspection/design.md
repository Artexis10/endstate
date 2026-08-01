## Context

Profiles contain app declarations plus legacy and generation-aware captured settings. Existing GUI correlation data is catalog metadata and cannot prove that a saved profile owns a settings module. Inspection therefore needs a standalone engine read boundary: it resolves the saved manifest and its permitted artifact-side context, then returns presentation-ready semantics without probing the host or executing restore/planning behavior.

The command is additive in JSON schema 1.x and remains under the existing top-level `profile` command. The engine, not the GUI, is the source of truth for profile membership, ownership, association, labels, warnings, and summary counts.

## Goals / Non-Goals

**Goals:**

- Return deterministic, non-null app, settings-app, and warning arrays for one extracted manifest path.
- Derive settings ownership from saved-profile evidence, including recursively resolved includes, with documented version-specific precedence.
- Associate owned settings with manifest apps only when evidence verifies the association, including catalog package-reference association for legacy short module IDs.
- Let a GUI capability-gate the feature and render engine-authored semantics without fallback parsing.

**Non-Goals:**

- Accepting or extracting `.endstate`/zip bundles, folders, or profile names.
- Detecting current-machine state, invoking drivers/matchers, planning, previewing, applying, restoring, or mutating any input.
- Treating current catalog membership as ownership evidence, or redesigning the generic command/capabilities schema.

## Decisions

### Extracted-manifest-only input

`profile inspect` accepts exactly one extracted manifest path. It can read that manifest; inspect-only recursively resolved relative `.json`, `.jsonc`, and `.json5` includes that remain inside the root manifest directory; sibling capture/config metadata; verified `provenance/modules/` snapshots; and the trusted current module catalog through its pure loader. Absolute, extensionless/profile-name, directory, bundle, and root-escaping includes fail with `MANIFEST_VALIDATION_ERROR`; inspection never extracts them. This preserves a bounded, side-effect-free artifact inspection boundary and avoids duplicating bundle extraction behavior in the GUI.

The alternative—accepting bundles, directories, or profile names—would couple inspection to discovery and extraction paths and make the GUI/CLI ownership boundary less explicit.

### Saved artifact is authoritative for ownership

Ownership first canonicalizes every module ID by trimming whitespace, lowercasing for comparison, and stripping exactly one leading `apps.`; no fuzzy matching occurs and raw IDs remain available as details. It is a union of applicable sources, deduplicated by canonical key: a stronger source records provenance but never suppresses modules found only in lower tiers. For v2 those sources are `configCaptures[].moduleId` and `legacyConfigLanes[].moduleId`. For v1 they are, strongest first, `restore[].fromModule`, `configModules[]`, sibling `metadata.json.configModulesIncluded[]`, and the first segment after `configs/` in restore `source`.

Inspection observes composition and applies only the root manifest's exact `refs.windows` `exclude` to Apps and root-only `excludeConfigs` to settings ownership; included manifests' exclusions are ignored. The pure trusted current catalog can enrich already-owned modules with labels and verified package-reference associations but MUST NOT create settings rows or run matchers.

This rejects the alternative of using catalog mappings as membership proof, which would make a newly installed catalog change the inventory of an unchanged profile.

### Conservative association and deterministic representation

Every owned module is attributed before mandatory grouping. Every `included` module sharing one unique Apps row is grouped, and every `not_in_profile` module sharing one verified absent-owner identity is grouped; `ambiguous` and `unresolved` modules are never grouped. Association states are exactly `included`, `not_in_profile`, `ambiguous`, and `unresolved`. Their field matrix is fixed: `ownerId` is non-null only for included/not-in-profile, `appId` only for included, `appIncluded` is true iff included, and `candidateAppIds` is the single app ID for included, sorted candidates for ambiguous, and empty otherwise. Row IDs are `app:<case-folded-app-id>`, `owner:<case-folded-owner-id>`, and `module:<canonical-module-key>` respectively. Only included marks an Apps row `hasSettings`.

Apps use captured `displayName`, then the first sorted package ref, then humanized app ID; package refs are all non-empty `refs` values, trimmed, deduplicated, and sorted. Settings use verified snapshot `displayName`, trusted-catalog `displayName`, associated app `displayName`, first sorted verified package ref, then a humanized canonical module key. `capturedAt` is non-empty manifest `captured`, then sibling metadata `capturedAt`, then null; conflicts can warn diagnostically. Apps and settings sort by case-folded display name then row ID; inner arrays sort by case-folded value then original value; warnings sort by code then message. Arrays are never null and summaries derive after grouping.

The alternative—collapsing uncertain modules into a likely app or hiding them—would overstate the inventory and force GUI logic to reconstruct uncertainty.

### Snapshot-first enrichment with explicit warning impact

`capturedEntryCount` is source-specific: v2 config captures sum `payloadManifest.length` once per distinct `captureId`; v2 legacy lanes count restore entries bound to the lane `legacyCaptureId`; v1 counts distinct restore-array entries attributed by `fromModule`, falling back per entry to `configs/<module>/...`. Metadata-only/configModules-only ownership counts zero. Grouped rows sum contributing module counts without double-counting an entry. Warnings contain engine-authored messages and a typed `diagnostic` or `inventory_incomplete` impact; the GUI does not classify prose.

The alternative—using raw provenance IDs or parsing warning text in the GUI—would expose technical identifiers by default and create an unstable cross-repository protocol.

### Capability-gated presentation

`features.profileInspection: true` advertises the capability. A GUI that does not see this feature renders an update-required state and does not infer settings ownership from manifests, catalog data, or machine state.

The alternative—probing a generic subcommand shape or falling back to a GUI parser—would violate the CLI-as-source-of-truth invariant.

## Risks / Trade-offs

- [Legacy artifacts lack consistent provenance] → Preserve ambiguous/unresolved rows and emit typed inventory-completeness warnings rather than guessing an owner.
- [Current catalog is stale or unavailable] → Keep saved-profile rows and use lower-precedence embedded/metadata labels; never add ownership.
- [Includes create duplicate evidence] → Deduplicate module ownership before rows and sort all arrays by stable documented keys.
- [Older bootstrapped engines lack the feature] → Gate on `features.profileInspection` and show update-required rather than degrading into client-side inference.

## Migration Plan

1. Add the `profile inspect` engine command, manifest-side reader, envelope output, and `features.profileInspection` capability.
2. Add engine schema and behavior tests for v1/v2 ownership, association, ordering, errors, and prohibited machine evaluation.
3. Update the GUI to capability-gate inspection and render the returned inventories, warnings, and counts verbatim.
4. Roll back by omitting the feature capability and command from a prior engine; compatible GUIs keep the update-required state and make no ownership claim.

## Open Questions

- None. The ownership sources, association states, label precedence, and presentation boundary are fixed by the cross-repository contract.
