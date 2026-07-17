## Context

Endstate deliberately identifies packages as `(driver, ref)` and treats an explicit `app.driver` as authoritative. That prevents nondeterministic fallback, but two entries can still describe the same physical application through different managers. Capture already preserves both and emits `possible_duplicate`; plan, apply, and verify currently expose no equivalent warning for hand-authored or edited manifests.

The warning must describe declarative intent before managed-package installation. Plan and dry-run provide the advisory before any mutation; live apply may perform backend preflight or consented bootstrap first, but constructs the advisory before executing managed-package install actions. Runtime package-manager names are not reliable for absent or unavailable packages, while public action labels fall back to refs and IDs and therefore cannot be treated as display-name evidence.

## Goals / Non-Goals

**Goals:**

- Warn deterministically when different resolved per-package drivers own entries with equal explicit manifest display names.
- Preserve every declaration, route, action, result, status, summary count, generation, and rollback owner.
- Reuse the existing additive `CommandWarning` and `possible_duplicate` vocabulary.
- Keep empty-warning output byte-compatible through `omitempty`.

**Non-Goals:**

- Selecting a preferred package manager or silently deduplicating entries.
- Blocking execution or requiring an override.
- Fuzzy matching, curated aliases, repository metadata lookup, or comparing refs, IDs, versions, fallback labels, or punctuation-normalized names.
- Adding streamed warning events or extending the warning to whole-set realizers such as Nix.

## Decisions

### Compare explicit declarative display names

The command layer will create an ownership observation only for a routed, non-manual per-package entry whose manifest `displayName` remains non-empty after outer whitespace trimming. Two observations collide only when their canonical resolved driver names differ and their trimmed names compare equal with `strings.EqualFold`.

This catches the risky hand-authored case before installation and remains stable when a driver is unavailable. Backend-detected labels are intentionally excluded: missing packages often have none, and using public action labels would accidentally compare fallback refs or IDs. The conservative false-negative for entries without `displayName` is preferable to inventing identity.

### Emit one warning on each later colliding entry

Observations are scanned in manifest order. A later observation emits at most one `possible_duplicate` warning when any earlier observation with another driver has the same name. The warning's optional `driver` and `ref` identify the later entry, its message states that both declarations were preserved, and three or more colliding drivers produce one warning for each later entry rather than pairwise growth.

This mirrors capture's deterministic later-entry convention while keeping warning order stable. Same-driver duplicates do not participate because they are not cross-manager ownership conflicts.

### Generate warnings after authoritative routing

The shared helper will accept resolved driver-lane observations rather than raw manifest strings. Plan's lane computation will return warnings beside its plan; apply will append them to existing preflight warnings; verify will add them to its result. Because apply filtering occurs before driver-lane execution, `apply --only` warns only when at least two colliding entries remain selected.

Backend preflight and a consented optional-backend bootstrap may therefore precede warning construction during live apply. This ordering is intentional: plan and dry-run are the pre-mutation advisory paths, while live apply guarantees the warning is constructed before managed-package installation rather than before every possible backend setup mutation.

The warning is envelope metadata only. No item event, phase event, status, reason, summary, install order, generation, or rollback behavior changes.

### Extend result payloads additively

`PlanResult` and `VerifyResult` gain `warnings,omitempty`, matching capture and apply. The existing schema version remains valid because consumers must ignore additive fields. GUI contract text will require advisory rendering without hiding either item.

### Alternatives rejected

- **Silent deduplication:** rejects explicit desired state and makes ownership depend on heuristics, contrary to the Terraform/Ansible-style declarative model.
- **Fuzzy or alias matching:** adds catalog policy and false positives without a stable cross-repository identity.
- **Backend-only runtime matching:** cannot warn before installing two absent packages and makes results depend on current machine state.
- **Failing validation:** overstates heuristic evidence and would break legitimate parallel declarations.

## Risks / Trade-offs

- **Entries without explicit display names are not detected** → Keep the warning honest and document that it is advisory, not a proof of uniqueness.
- **Two distinct packages may intentionally share a display name** → Never block or suppress either entry; include driver/ref provenance for review.
- **New warnings may appear in existing JSON consumers** → Use the existing warning shape and additive `omitempty` fields covered by the GUI's unknown-field compatibility rule.
- **Warning behavior could drift from capture** → Lock exact case-insensitive, outer-trim-only comparison and later-entry cardinality in shared command tests.

## Migration Plan

Ship as a backward-compatible engine update. No manifest or state migration is required. Rollback is removal of the additive warning fields/helper; package state is unaffected.

## Open Questions

None.
