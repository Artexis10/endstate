## 1. Warning Semantics

- [x] 1.1 Add red tests for exact trimmed case-insensitive matching, same-driver exclusion, empty/similar-name exclusion, later-entry provenance, and deterministic three-driver cardinality.
- [x] 1.2 Implement the shared resolved-lane ownership observation helper and make the helper tests green.

## 2. Command Integration

- [x] 2.1 Add red plan tests proving `possible_duplicate` is additive, both actions remain, and empty warnings are omitted from JSON.
- [x] 2.2 Add red apply tests proving dry-run/live results preserve actions and summaries, append existing warnings, and `--only` considers only selected entries.
- [x] 2.3 Add red verify tests proving both results remain and non-package/manual entries do not participate.
- [x] 2.4 Propagate duplicate warnings through plan, apply, and verify result payloads and make the command tests green.

## 3. Contracts and Verification

- [x] 3.1 Update the CLI JSON and GUI integration contracts for advisory `possible_duplicate` warnings on plan/apply/verify; leave the event and manifest contracts unchanged.
- [x] 3.2 Run formatting, targeted command/planner tests, the full feasible Go suite, and strict OpenSpec validation; record unrelated sandbox-only baseline failures separately.
- [x] 3.3 Complete an independent contract/code review and resolve every blocking or should-fix finding.
