## Why

`apply` converges the declared package set but leaves **no durable, inspectable record of
what it committed**. There is run history (`state/runs/`), but no numbered package-set
record that can be listed and (in a later phase) rolled back. Phase 1 (`nix-realizer-backend`,
PR #50) added a Nix `Realizer` beside the winget `Driver`, so two package backends now
exist with **no shared state or UX layer** above them.

This change adds an **engine-owned Provisioning Generation**: a numbered, self-describing
record of the committed package set, written for **both** backends after a successful
`apply`. It is the unification layer where the cross-OS model becomes concrete — and where
**Windows gains an install audit trail for the first time**. No rollback is introduced here
(that is a later phase); this change pins the generation format so it is stable before a
rollback phase writes against it.

This overrules the previously implicit assumption that `apply` leaves no durable package
record. It also makes two `AI_CONTRACT.md` Non-Goals partially outdated — **#2 "No
cross-platform parity yet"** and **#4 "No automatic rollback"** (this change is a
prerequisite for the later, opt-in, `--confirm`-gated rollback). `AI_CONTRACT.md` is a
PROTECTED file; this is **flagged for the maintainer**, not edited here.

## What Changes

- Add a new package **`internal/provision`** that owns the `Generation` record (with its
  **own** `SchemaVersion`, mirroring `internal/state`), a `Capabilities` value, and the
  optional `CapabilityReporter` / `Rollbacker` interfaces (discovered by type-assertion
  exactly like `driver.BatchDetector`). `Rollbacker` is **declared but not implemented**
  in this change.
- `apply` writes a Provisioning Generation after a successful run, for **both** backends,
  under `state/generations/` resolved via the config path resolver (never hardcoded), using
  the existing atomic temp-file + rename idiom.
  - **Atomic backend (Nix):** commit a generation **only on full success** (the profile
    generation advanced); `Partial=false`; `Native` = the Nix generation number.
  - **Non-atomic backend (winget):** record the successfully-installed subset;
    `Partial=true` when any attempted install failed; `Native=""`.
  - A generation is written **only when the committed set advanced** (at least one ref
    installed this run). Idempotent re-runs add nothing.
- `AddedRefs` records **only** refs whose status this run was `installed` (never `present`).
- Add a read-only **`generations`** command that lists recorded generations (newest first).
- Implement `CapabilityReporter` on the Nix realizer (atomic + native-rollback +
  batch-install) and the winget driver (all false), so a later phase can discover rollback
  eligibility by type-assertion.

The dual `Driver` + `Realizer` interfaces are **kept beside each other**; only the
state/UX is unified here. Folding winget into a single converge interface is explicitly out
of scope (later, riskier phase).

## Capabilities

### New Capabilities

- `provisioning-generation`: `apply` persists a numbered, versioned, install-only
  Provisioning Generation for both backends under the resolved state directory; a read-only
  `generations` command lists them. Atomic backends commit only on full success; non-atomic
  backends record the installed subset and mark it partial.

### Modified Capabilities

- `separation-of-concerns`: the *Distinct Pipeline Stages* requirement gains a scenario
  asserting the Provisioning Generation is **install-only** — it records package state only
  and never reads or writes `state/backups/` or the restore revert journal, and the verify
  stage never writes generations. This keeps `rollback` (packages, future) and `revert`
  (configs) distinct over distinct logs.

## Impact

- `internal/provision/` (new) — `Generation` + `ProvItem` types, `SchemaVersion` const,
  `Capabilities` + `CapabilityReporter` + `Rollbacker` interfaces, `Dir()` (resolved via
  `state.StateDir()`), `Write`/`List`/`nextNumber` (atomic temp+rename; `.tmp`-excluded
  listing — mirrors `state.SaveRunHistory` / `state.ListRunHistory`).
- `internal/commands/apply_realizer.go` — write a generation at the successful-apply return
  site (Nix path) when `Result.Advanced`.
- `internal/commands/apply.go` — write a generation at the driver-path return site (winget)
  when ≥1 ref was installed this run; set `Partial` from per-item failures.
- `internal/commands/generations.go` (new) — `RunGenerations` + `runGenerationsList`,
  modeled on `report.go`.
- `internal/realizer/nix/nix.go` + `internal/driver/winget/` — implement
  `provision.CapabilityReporter`.
- `cmd/endstate/main.go` — **PROTECTED**: add `case "generations"` in `dispatch()` + a
  usage line, mirroring `report`/`profile`. **Flagged for explicit go-ahead.**
- `docs/contracts/cli-json-contract.md` — **PROTECTED**: additive `generations` command +
  envelope `data` rows for the generation list. **Flagged for explicit go-ahead.**
- No manifest/envelope schema-version bump: the `Generation` record carries its **own**
  schema version, and the `generations` command + its fields are additive per
  `schema-versioning`.
