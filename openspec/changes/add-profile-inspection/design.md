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

`profile inspect` accepts exactly one extracted manifest path. It can read that manifest, its recursively resolved includes, sibling capture/config metadata, and verified `provenance/modules/` snapshots. This preserves a bounded, side-effect-free artifact inspection boundary and avoids duplicating bundle extraction behavior in the GUI.

The alternative—accepting bundles, directories, or profile names—would couple inspection to discovery and extraction paths and make the GUI/CLI ownership boundary less explicit.

### Saved artifact is authoritative for ownership

For v2, ownership is the deduplicated union of distinct `configCaptures[].moduleId` values and explicitly declared legacy config lanes. For v1, evidence is considered in descending authority: explicit `restore[].fromModule`, declared captured config-module metadata, sibling capture metadata, then old `configs/<module-id>/...` restore sources. Catalog data can enrich an already-owned module with names and package-reference associations but MUST NOT create settings rows.

This rejects the alternative of using catalog mappings as membership proof, which would make a newly installed catalog change the inventory of an unchanged profile.

### Conservative association and deterministic representation

Every owned settings module becomes one settings-app row before owner grouping. A row is grouped only when all grouped modules share one verified owner; ambiguous and unresolved modules remain separate. Association states are exactly `included`, `not_in_profile`, `ambiguous`, and `unresolved`; only `included` marks an Apps row `hasSettings`. Arrays are never null and have documented deterministic ordering, allowing summary values to be derived exclusively from finalized arrays.

The alternative—collapsing uncertain modules into a likely app or hiding them—would overstate the inventory and force GUI logic to reconstruct uncertainty.

### Snapshot-first enrichment with explicit warning impact

Verified embedded snapshots have friendly-label precedence, followed by captured module metadata, catalog labels for an already-owned module, associated manifest-app labels/package refs, then deterministic humanized short IDs. Warnings contain engine-authored messages and a typed `diagnostic` or `inventory_incomplete` impact; the GUI does not classify prose.

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
