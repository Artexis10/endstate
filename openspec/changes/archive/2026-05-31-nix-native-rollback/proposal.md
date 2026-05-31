## Why

Phase 2 (`provisioning-generation`, PR #58, merged) made `apply` commit a numbered,
inspectable **Provisioning Generation** for both package backends and shipped a read-only
`generations` command. It deliberately **declared** the `provision.Rollbacker` interface
but left it unimplemented, and the Nix realizer already advertises
`Capabilities{NativeRollback: true}`. So the unification layer exists, the capability is
advertised, and there is a stable, numbered history — but there is **no way to revert** the
package set to a prior generation.

This change implements **native Unix rollback**: a top-level `rollback` command that reverts
the installed package set to a prior Provisioning Generation, for backends that advertise
`NativeRollback` (the Nix realizer today). It implements `provision.Rollbacker` on the Nix
realizer via `nix profile rollback`. Backends that do not advertise native rollback (the
winget driver on Windows; any host with no realizer) **refuse cleanly and change nothing** —
best-effort winget rollback is a later phase (Phase 4), explicitly out of scope here.

`rollback` is a **top-level command**, not an `apply` flag, keeping the install-revert verb
out of the config-bearing apply flow. It operates on **packages only**: it is distinct from
`revert` (configuration) and never touches `state/backups/` or the restore revert journal.

This updates `AI_CONTRACT.md` Non-Goal **#4** ("No automatic rollback"): a manual, opt-in,
`--confirm`-gated **package** rollback now exists alongside the manual `revert` for configs, so
#4 is reworded to state that all rollback is explicit/opt-in and nothing auto-rolls-back (the
no-*automatic*-rollback principle is preserved). `AI_CONTRACT.md` is PROTECTED; this edit is
made at the maintainer's explicit request.

## What Changes

- Implement `provision.Rollbacker` on the Nix realizer (`internal/realizer/nix`):
  `Rollback(to int) error` runs `nix profile rollback --profile <profile>` (previous version)
  or `... --to <native>` (a specific Nix version), classifying failures to engine error codes
  via the existing `classify(...)` path. Raw Nix text is confined to `error.detail` (the moat).
- Add a top-level **`rollback`** command (`internal/commands/rollback.go`):
  - Acquires the host realizer via the existing `newRealizerFn()` seam (Windows has none →
    refuses). Discovers rollback eligibility by **type-assertion** on `provision.Rollbacker`
    and `provision.CapabilityReporter.Capabilities().NativeRollback` — exactly like
    `driver.BatchDetector`.
  - **Target identity = engine generation number.** `rollback --to <N>` maps engine
    Provisioning Generation N → its recorded `Native` (the Nix version), then rolls back to
    that native version. Bare `rollback` reverts to the previous version. The user never
    references a Nix version directly (the moat).
  - **Requires `--confirm`.** Default refuses (changes nothing); `--dry-run` previews the
    resolved target without confirmation or mutation. Consistent with `non-destructive-defaults`
    and symmetric with Phase 4's planned `--confirm`-gated winget rollback.
  - **Appends a new Provisioning Generation** snapshotting the now-active set after a
    successful rollback (Native = the active Nix version; `addedRefs` empty; marked as
    rollback-produced), so the engine's append-only history keeps "newest = currently active"
    truthful and provides an audit trail. (Empirically, `nix profile rollback` repoints to an
    *existing* older Nix version rather than minting a new one; the new record is therefore an
    *engine* generation whose `Native` points back at that older version.)
- Add an optional `Rollback bool` field to `provision.Generation` to mark rollback-produced
  records (additive; `SchemaVersion` stays `"1.0"`).
- Add three additive engine error codes: `ROLLBACK_UNSUPPORTED`, `GENERATION_NOT_FOUND`,
  `ROLLBACK_FAILED`.

The dual `Driver` + `Realizer` interfaces are **kept beside each other** — unchanged.

## Capabilities

### New Capabilities

- `nix-native-rollback`: a top-level `rollback` command reverts the installed package set to a
  prior Provisioning Generation, for backends that advertise native rollback. It is identified
  by engine generation number (mapped to the backend-native anchor), gated by `--confirm` with
  a `--dry-run` preview, appends a new Provisioning Generation on success, operates on packages
  only, and confines raw backend diagnostics to the error detail. Non-native-rollback backends
  refuse without changing state.

### Modified Capabilities

- `separation-of-concerns`: **ADDS** a new requirement ("Rollback operates on packages only")
  asserting that `rollback` never touches `state/backups/` or the restore revert journal and
  never invokes restore, and that `rollback` (packages) and `revert` (configs) stay distinct
  verbs over distinct logs. This is an **ADDED requirement** (a brand-new requirement name), NOT
  a modification of the existing *Distinct Pipeline Stages* requirement — the not-yet-archived
  `provisioning-generation` change already MODIFIES that requirement, and a second concurrent
  MODIFY of the same requirement would risk dropping a scenario at archive time. Also enforced
  in code by a guard test (`TestRollbackStaysPackageOnly`).

## Impact

- `internal/realizer/nix/rollback.go` (new) — `Rollback(to int) error` on `*Backend` via
  `nix profile rollback`; classified failures through `classify(...)`.
- `internal/commands/rollback.go` (new) — `RunRollback` + `RollbackFlags` + `RollbackResult`,
  modeled on `generations.go` / `apply_realizer.go` (reuses `newRealizerFn`, `provision.List`,
  `r.Current`, the `realizerEnvelopeError`/`isSystemic` moat pattern).
- `internal/provision/provision.go` — additive optional `Rollback bool` field on `Generation`.
- `internal/envelope/errors.go` — `ErrRollbackUnsupported`, `ErrGenerationNotFound`,
  `ErrRollbackFailed`.
- `cmd/endstate/main.go` — **PROTECTED (maintainer-approved, additive)**: `case "rollback"` in
  `dispatch()` + a `usageText` line + `commandUsage("rollback")`. (`--to`/`--confirm`/`--dry-run`
  parsing already exists in `parseArgs`.)
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**:
  `## Command: rollback` section + the three new error codes + a note on the optional `rollback`
  field on generation records.
- `docs/ai/AI_CONTRACT.md` — **PROTECTED (maintainer-approved)**: reword Non-Goal #4 to reflect
  that a manual, opt-in `rollback` exists while preserving the no-*automatic*-rollback principle.
- `openspec/changes/nix-native-rollback/specs/separation-of-concerns/spec.md` — ADDED
  requirement asserting rollback is package-stage only.
- No manifest/envelope schema-version bump: the `Generation` record carries its own schema
  version; the `rollback` command, its fields, and the new error codes are additive per
  `schema-versioning`.
- **Zero Windows behavior regression**: `rollback` is a brand-new verb; on Windows (no
  realizer) it returns a clean `ROLLBACK_UNSUPPORTED`. Proven by host-aware tests +
  `GOOS=windows` build/vet.
