## ADDED Requirements

### Requirement: Runtime cross-driver ownership warnings are advisory
Plan, apply, and verify SHALL emit a `possible_duplicate` warning when two routed non-manual package entries use different resolved per-package drivers and have non-empty explicit manifest display names that are equal after outer whitespace trimming and case-insensitive comparison. Both entries MUST remain present and independently routed through their declared drivers. The warning MUST NOT change execution, status, reason, summary counts, generation recording, rollback ownership, or fallback behavior.

Refs, IDs, versions, fallback labels, substrings, punctuation-normalized names, and other fuzzy similarity MUST NOT be treated as duplicate evidence. Warning order SHALL follow manifest order, and each later colliding entry SHALL produce at most one warning identifying that entry's driver and ref.

#### Scenario: Equal display names across drivers warn without suppression
- **WHEN** a manifest routes two package entries through different per-package drivers and their non-empty explicit display names are equal after trimming and case-insensitive comparison
- **THEN** plan, apply, and verify include `possible_duplicate`
- **AND** both package actions or results remain present with their authoritative drivers

#### Scenario: Warning never changes desired state
- **WHEN** a cross-driver duplicate warning is emitted
- **THEN** neither entry is deduplicated, rerouted, blocked, or assigned a different status or summary outcome because of the warning

#### Scenario: Similar or inferred labels do not warn
- **WHEN** cross-driver entries have empty explicit display names or names that differ under exact trimmed case-insensitive comparison
- **THEN** refs, IDs, versions, fallback labels, punctuation, substrings, and fuzzy similarity produce no duplicate warning

#### Scenario: Same-driver equality does not warn
- **WHEN** two entries resolve to the same package driver and have equal display names
- **THEN** no cross-driver ownership warning is emitted

#### Scenario: Later-entry warnings are deterministic
- **WHEN** three differently routed package entries share the same qualifying display name
- **THEN** the first entry emits no warning and each later entry emits at most one warning in manifest order

#### Scenario: Filtered apply considers only selected entries
- **WHEN** apply filtering selects only one member of an otherwise colliding pair
- **THEN** apply emits no duplicate warning for the excluded entry
