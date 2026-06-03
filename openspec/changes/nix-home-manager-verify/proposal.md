## Why

The Nix config arc covers apply (activates home-manager config) and capture (records the declared config for
round-trip). But `verify` completely ignores home-manager config even when the manifest declares a
`homeManager` block and a config was previously applied. This means a machine whose home-manager generation
has drifted (e.g. another tool ran `home-manager switch`, or the config was never applied) reports all-green
from `verify` — a false positive. Close this gap: when the manifest declares a home-manager input, `verify`
should assert that the active generation matches the recorded one.

## What Changes

- **`verify` gains a home-manager config check** on the realizer path only (winget/driver path unchanged).
  The check is evaluated only when `mf.HomeManager != nil`.
- **Semantics: presence + drift-vs-recorded.** The check reads the active home-manager generation number
  (from the `$XDG_STATE_HOME/nix/profiles/home-manager` symlink) and the recorded generation number (most
  recent Provisioning Generation whose `HomeManager` is non-nil). If active == recorded → `pass`. If
  active != recorded and active > 0 → `fail config_drift`. If active == 0 (nothing active) → `fail missing`.
- **Seam (hermetically testable).** A new optional realizer capability `HomeGenerationReader` (discovered by
  type-assertion, exactly like `Pruner`/`HomeActivator`/`HomeRollbacker`) lets the Nix backend expose the
  active generation number. The verify path type-asserts it; a backend without it skips the hm check. The
  Nix `Backend` implements it wrapping the existing `homeGen()` function. `fakeRealizer` gains a scriptable
  field.
- **One VerifyItem** of type `"home-manager"`, id `"home-manager"`, with status `pass`/`fail`, reason
  (`config_drift` or `missing`), a human message, and the active/expected generation numbers exposed via
  the existing `Version`/`Expected` fields.
- **Summary counts** include the home-manager item.

## Capabilities

### New Capabilities

- `nix-home-manager-verify`: when the manifest declares a `homeManager` block, `verify` on the Nix realizer
  checks the active home-manager generation against the most-recently recorded one and reports
  `pass` / `fail config_drift` / `fail missing`.

## Impact

- `internal/realizer/realizer.go` — new optional `HomeGenerationReader { ActiveHomeGeneration() int }`.
- `internal/realizer/nix/home_manager.go` — implement `ActiveHomeGeneration()` on `*Backend` wrapping
  `homeGen()`; compile-time assertion `var _ realizer.HomeGenerationReader = (*Backend)(nil)`.
- `internal/commands/verify_plan_realizer.go` — add the hm check in `runVerifyRealizer`.
- `internal/commands/verify_plan_realizer_test.go` — new hermetic test cases.
- `internal/commands/apply_realizer_test.go` — extend `fakeRealizer` with `ActiveHomeGeneration`.
- `openspec/specs/nix-home-manager-verify/spec.md` — new spec.

## Non-Goals

- No change to the winget/driver verify path.
- No change to `apply` or `capture` behavior.
- No automatic remediation (the check is read-only; `verify` is always read-only).
- No per-file config drift check (this is generation-level only).
