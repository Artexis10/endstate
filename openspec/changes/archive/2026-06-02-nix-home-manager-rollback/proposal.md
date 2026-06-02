## Why

The Nix config arc is complete except one gap: there is no way to roll the home-manager **config**
back. Packages roll back natively (`nix-native-rollback` → `nix profile rollback`), and config can be
applied (#81), wrapped from a `home.nix` (#87), and captured (#86/#89). But once a user applies config
A and then config B, `rollback` reverts only the package set — the home-manager config stays at B. This
change closes the loop: a Provisioning Generation N can be reverted to as a coherent whole — its package
set **and** the home-manager config it recorded.

Rollback of the config was explicitly deferred from #81; this is that deferred work, parallel to package
rollback (#60 native `nix profile rollback --to`; #63 winget best-effort).

## What Changes

- **`rollback` gains an opt-in config stage**, gated by the existing `--enable-restore` flag — symmetric
  with `apply --enable-restore`. `rollback --to N` stays **package-only by default**; with
  `--enable-restore` it *also* re-activates the home-manager config recorded in generation N. One coherent
  "rollback to N". If generation N recorded no config, the config is left unchanged (non-destructive).
- **Re-activation mechanism (empirically confirmed):** home-manager has no arbitrary pointer-move-back
  (`switch --rollback` is one step only). The engine resolves the recorded home-manager generation's
  snapshot (`home-manager-<M>-link`) and runs its `activate` script, which mints a **new forward**
  home-manager generation that reproduces the recorded config — exactly the append-only "newest == active"
  model package rollback already uses.
- **New optional realizer capability `HomeRollbacker`**, discovered by type-assertion exactly like
  `Pruner` / `HomeActivator`; only the Nix backend implements it.
- **Garbage-collected snapshot handling:** if the recorded generation's store snapshot is gone, the engine
  falls back to re-activating a *directly-referenced* flake (faithful for a pinned flake), and otherwise
  refuses without changing the config (the engine-generated wrapper directory is overwritten each apply, so
  rebuilding from it would be unfaithful) — non-destructive default with a clear remediation.
- The appended rollback Provisioning Generation records the now-active config, so `newest == active`
  stays truthful for config as well as packages. Raw home-manager/Nix text stays confined to `error.detail`.

## Capabilities

### New Capabilities

- `nix-home-manager-rollback`: `rollback --enable-restore` reverts the home-manager config recorded in the
  target Provisioning Generation by re-activating that generation's recorded home-manager generation
  (append-only forward generation), with a non-destructive fallback when the snapshot is unavailable, and
  records the reverted config in the appended generation.

### Modified Capabilities

- None in spec terms. This extends the existing `rollback` command (`nix-native-rollback`) with an opt-in
  config stage; the package rollback path, the winget best-effort path, and a bare (package-only) rollback
  are unchanged.

## Impact

- `internal/realizer/realizer.go` — new optional `HomeRollbacker { RollbackHome(generation int) (newGen int, err error) }`.
- `internal/realizer/nix/` — implement `RollbackHome` (resolve `home-manager-<M>-link` → run `<store>/activate`),
  reusing `homeProfilePath`/`homeGen`/`parsePlainLog`/`classify`; a small script-exec seam (`activate` is a
  store-path executable, not a `nix` subcommand).
- `internal/commands/rollback.go` — `RollbackFlags.EnableRestore`; `RollbackResult` config sub-object; the
  realizer path validates config eligibility before mutating, then reverts packages and (opt-in) config, and
  records the reverted config. Stays **package/realizer-only** — never imports `internal/restore`.
- `cmd/endstate/main.go` — **PROTECTED (additive)**: wire `EnableRestore` into the rollback command and update
  the `rollback` usage text.

## Non-Goals (deferred)

- **Config-file restore rollback** (the `state/backups/` + revert-journal layer) — a separate concern;
  this change is home-manager generation re-activation only, distinct from file restore.
- **Best-effort config rollback on non-realizer backends** (winget has no home-manager). Config rollback is
  realizer-only; package rollback on winget is unchanged.
- **Rolling back to a *pre-config* state by removing the home-manager config** (un-applying home-manager).
  We only re-activate a *recorded* config; a generation with no recorded config leaves the config unchanged.
