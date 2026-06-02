## Context

`nix-native-rollback` (#60) gave the `rollback` command its native package path: map an engine-owned
Provisioning Generation number N → the recorded `Native` anchor → `nix profile rollback --to`, then append a
rollback-marked generation. `nix-home-manager-config` (#81/#87) records, per apply, the activated
home-manager configuration in `provision.HomeGenRef{Flake, Config, Generation}`. Config rollback was deferred
from #81. This change reverts the **config** to a prior generation, parallel to the package path.

## Empirical verdict (real-nix smoke, confirmed before this spec was finalized)

home-manager has **no arbitrary pointer-move-back**. `home-manager switch --rollback` is `nix-env --rollback`
— one step back only. To revert to an arbitrary generation M, you run that generation's `activate` script
(`<store-path>/activate`). Confirmed in a sandbox (apply marker "A" → gen1, apply marker "B" → gen2, run
gen1's `activate`): a **new forward** generation link (`home-manager-3-link`) appeared and became active, and
the managed file reverted to "A". So re-activation is **append-only** ("newest == active"), exactly the model
package rollback uses. The activate script runs directly (no flags, driver-version 0 default) with exit 0,
emitting plain-text activation lines (`Activating installPackages` / `linkGeneration`) — classified by the
existing `parsePlainLog` + `classify` path.

## Decisions

- **Coupled, opt-in via `--enable-restore`.** `rollback --to N` is package-only by default; with
  `--enable-restore` it also reverts the home-manager config recorded in generation N. Symmetric with
  `apply --enable-restore` (which gates the config *apply* stage). No new rollback-specific flag, no new
  command.
- **Re-activate the recorded generation's store-path snapshot (primary).** Resolve
  `home-manager-<M>-link` (M = `genN.HomeManager.Generation`), read the store path, run `<store>/activate`.
  The `-<M>-link` store path is the **immutable, faithful** snapshot of the config at gen N. (The
  engine-generated wrapper-flake dir is regenerated/overwritten each apply, so rebuilding *from it* would be
  unfaithful — this is the same reason #89 made capture prefer `Config` over the generated `Flake`.)
- **Fallback when the snapshot link is gone (GC'd):**
  - *Directly-referenced flake* (`genN.HomeManager.Config == ""`): re-activate `genN.HomeManager.Flake` via
    the existing `ActivateHome` — a pinned flake rebuilds faithfully.
  - *Engine-generated wrapper* (`Config != ""`): **refuse** with `ROLLBACK_FAILED` + remediation ("generation
    N's home-manager snapshot was garbage-collected; re-apply with `endstate apply --enable-restore`"). The
    state-dir wrapper now holds the *latest* config, not gen N's, so silently activating it would be wrong —
    non-destructive default.
- **New optional `realizer.HomeRollbacker`** — `RollbackHome(generation int) (newGen int, err error)` —
  discovered by type-assertion like `Pruner`/`HomeActivator`. Keeps interfaces small; only Nix implements it.
  A missing `-<M>-link` is surfaced distinctly so the command layer (which holds the recorded `HomeGenRef`)
  can drive the fallback.
- **Validate-before-mutate.** When `--enable-restore` is set and gen N recorded a config but the backend is
  not a `HomeRollbacker`, refuse with `ROLLBACK_UNSUPPORTED` **before** touching packages — nothing mutates.
- **Append one combined generation.** On success, append a single rollback-marked Provisioning Generation
  recording the now-active package set **and** `HomeManager{Flake, Config (copied from gen N), Generation:
  newHmGen}`, keeping `newest == active` truthful for config too.
- **Separation of concerns.** Config rollback is home-manager *generation re-activation* (a realizer +
  provisioning concern). It is distinct from config-*file* restore (`state/backups/` + the revert journal)
  and `rollback.go` continues to **not** import `internal/restore` (the `TestRollbackStaysPackageOnly` guard
  stays green). Raw backend text stays in `error.detail`.

## Design

### Realizer (`internal/realizer`)
`HomeRollbacker { RollbackHome(generation int) (newGen int, err error) }`. Compile-time-asserted on the Nix
`Backend`.

### Nix backend (`internal/realizer/nix`)
`RollbackHome(M)`:
1. `link := homeProfilePath() + "-" + strconv.Itoa(M) + "-link"`. If it does not exist → return a
   distinguishable error (the command falls back). 
2. `store := readlink(link)`; run `runScript(filepath.Join(store, "activate"))` via a small injectable
   exec seam (the `activate` script is a store-path executable, not a `nix` subcommand, so it is a separate
   seam from the nix `Runner`; defaults to `exec.Command`).
3. Classify via `parsePlainLog` + `classify` (exit<0 → REALIZER_UNAVAILABLE/spawn; non-zero → anchor table;
   the `INSTALL_FAILED` fallback is **remapped to `ROLLBACK_FAILED`** so the error names the verb — exactly
   as the package `Rollback` does). On success return `homeGen()` (the new forward generation).

### Command (`internal/commands/rollback.go`)
`RollbackFlags.EnableRestore bool`; `RollbackResult` gains a `HomeManager *RollbackHomeResult` sub-object
(target hm generation M, new hm generation, flake/config, dry-run-aware). In `runRealizerRollback`, after
resolving the package target and **before** any mutation:
- `wantConfig := flags.EnableRestore`; load gen N; `home := genN.HomeManager`.
- If `wantConfig && home != nil && r` is not a `HomeRollbacker` → `ROLLBACK_UNSUPPORTED` (nothing mutated).
- `--dry-run` → report package target + (if `wantConfig && home != nil`) the config target M; activate nothing.
- `--confirm` gate (existing).
- `rb.Rollback(target)` (packages). Then if `wantConfig && home != nil`: `hr.RollbackHome(home.Generation)`;
  on the distinct "snapshot missing" signal, fall back (direct flake → `ActivateHome(home.Flake)`; wrapper →
  `ROLLBACK_FAILED` + remediation). A config failure after a package rollback surfaces the classified error
  (mirrors apply's package-then-config ordering).
- Append one combined rollback generation (extend `appendRollbackGeneration` to take an optional
  `*provision.HomeGenRef`, set to `{Flake, Config from gen N, Generation: newHmGen}`).

### CLI (`cmd/endstate/main.go`, protected — additive)
Add `EnableRestore: p.enableRestore` to the rollback case (the parser field already exists and is used by
apply); extend the `rollback` usage text to note that `--enable-restore` also reverts the recorded
home-manager config.

## Risks / Verification

- **GC'd snapshot.** Handled: faithful fallback for a direct flake; honest refusal for the engine-generated
  wrapper. Tested with the distinct missing-generation signal.
- **Partial outcome** (package rollback succeeds, config rollback fails). Mirrors apply: the classified config
  error is returned (raw in detail). Documented.
- **Hermetic tests:** realizer `RollbackHome` (temp `XDG_STATE_HOME` + a `home-manager-<M>-link` symlink;
  injected `runScript`; classification of eval/permission/daemon/spawn; missing-link → distinct error, no
  exec). Command tests (extend `fakeRealizer` to a `HomeRollbacker`): coupled success records the new config;
  no `--enable-restore` ⇒ no `RollbackHome`; no recorded config ⇒ package-only; non-`HomeRollbacker` + config
  ⇒ `ROLLBACK_UNSUPPORTED`, nothing mutated; dry-run previews and does not call; config failure classification
  with raw confined to detail; fallback (direct flake re-activates; wrapper refuses). `go test ./...` (Linux);
  `GOOS=windows go build ./...` + `go vet`; openspec strict.
- **Real-nix e2e smoke** (sandbox `$HOME` + `ENDSTATE_ROOT`, hm pin `git+file:///home/hugoa/projects/home-manager`):
  apply config A (gen1) → apply config B (gen2) → `rollback --to 1 --enable-restore --confirm` → the managed
  config reverts to A and a rollback-marked generation recording the home ref is appended. Real `$HOME`
  untouched.
