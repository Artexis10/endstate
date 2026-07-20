## Why

End-to-end verification of the setup flow on a real machine found that it reports success for work it never performed. On default settings the GUI runs `apply --dry-run`, installs nothing, and renders "Setup complete"; the settings picker offers 41 modules for a profile that carries 8; and the results list is never reconciled against the envelope because the GUI reads apply fields that do not exist. The engine's apply path is correct — verified dry-run → install → independent confirmation → idempotent re-run — so every defect sits in the reporting and scoping layers at the GUI↔engine seam.

These are launch-blocking. The product's core promise is "rebuild this machine", and today the default path silently no-ops while claiming it succeeded.

## What Changes

- **BREAKING (GUI default):** `dryRunEnabled` defaults to `false`, so the primary action performs a real apply. Users who deliberately enabled dry run keep it — only the shipped default changes.
- Dry-run state becomes visible. The apply envelope already carries `dryRun`; result surfaces SHALL consume it and SHALL NOT present a dry run as completed setup.
- `restoreModulesAvailable` becomes profile-scoped. It currently answers "which catalog modules match these apps"; it SHALL answer "which settings does this profile actually carry". Measured impact on a real profile: 41 offered → 8 real, eliminating 33 phantom entries (80%).
- `restoreModulesAvailable` entries gain an `entryCount` so consumers can show how much each module carries. The GUI's per-module file hint is currently dead code (it renders only when a hardcoded `0` is exceeded), which is precisely why the phantom entries were invisible.
- The GUI consumes contract-defined apply fields. It currently reads `counts` and `items` — fields defined for `capture` and `generations` respectively, never for `apply` — and marks the contract's real `summary` field "legacy". Consequence: `items` is always empty, so final-state reconciliation has never run and stale plan-phase `to_install` rows survive into the results screen.
- The stale `items[]` references in the `apply-restore-envelope` spec are corrected to `actions[]`, matching both the contract and the engine.

## Capabilities

### New Capabilities
- `apply-dry-run-disclosure`: a dry run SHALL be distinguishable from a real apply in every result surface; consumers SHALL NOT report installs, completion, or success for a run that changed nothing.
- `restore-modules-profile-scoping`: `restoreModulesAvailable` SHALL list only modules the profile actually carries restorable payload for, not every catalog module matching the app list.

### Modified Capabilities
- `restore-modules-display-name`: entries gain a required `entryCount` field alongside `id` and `displayName`.
- `apply-restore-envelope`: scenarios referencing the apply envelope's `items[]` are corrected to `actions[]`; the contract and engine have always used `actions[]`, so the spec is the outlier.
- `cli-source-of-truth`: adds a requirement that the GUI consume only envelope fields the CLI contract defines for that command — the guard that would have caught this class of drift.

## Impact

**Engine (`go-engine/`)**
- `internal/commands/apply.go` — `restoreModulesAvailable` construction (currently `modules.MatchModulesForApps(catalog, mf.Apps)` with no profile-payload check); `RestoreModuleRef` gains `entryCount`.
- Fan-out to the other apply lanes that thread `restoreModulesAvailable`: `apply_brew_only.go`, `apply_realizer.go`, `apply_generation.go`.

**GUI (`endstate-gui/`, separate repo)**
- `src/settings.ts` — `DEFAULT_SETTINGS.dryRunEnabled`.
- `src/types.ts` — `EndstateApplyData` field definitions.
- `src/App.tsx` — result assembly reading `counts`/`items`; dry-run plumbed into `ApplyResult`.
- `src/lib/apply-utils.ts` — reconciliation against `actions[]`; stop dropping `name`/`driver` when rebuilding events.
- `src/components/app/intent/setup-flow.tsx` — results header; real `entryCount` instead of hardcoded `0`.

**Contracts**
- `docs/contracts/cli-json-contract.md` — document `entryCount`; clarify that `counts`/`items` are not apply fields.
- `docs/contracts/gui-integration-contract.md` — dry-run disclosure obligation for consumers.

**Compatibility**
- `entryCount` is an additive envelope field; older GUIs ignore it. Scoping `restoreModulesAvailable` narrows an existing array rather than changing its shape, so no schema version bump is required. The GUI change is the coordinated half and needs the engine version gate already used for capability handshakes.
