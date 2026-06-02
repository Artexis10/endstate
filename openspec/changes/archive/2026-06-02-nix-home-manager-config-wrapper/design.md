## Context

`nix-home-manager-config` (shipped, #81) added the realizer config stage: `homeManager.flake` →
`nix run <home-manager-pin> -- switch --flake <ref> -b endstate-backup`, classified through the anchor
path, recorded in the Provisioning Generation as `homeManager{flake,generation}`. Its proposal named a
"home.nix wrapper" as the way to hide the flake. This change builds that wrapper.

## Goals / Non-Goals

**Goals:** let a user supply only a `home.nix`; the engine generates the surrounding flake; invisible by
default but inspectable + ejectable; reuse the #81 activation, pins, and recording unchanged.

**Non-Goals (deferred):** config capture; the Endstate module catalog; the zero-Nix native schema (declare
in Endstate's own format → generate the `home.nix`); multiple modules / a config directory.

## Decisions

- **Input = `homeManager.config`** — a path to a `home.nix`, resolved relative to the manifest dir.
  **Mutually exclusive** with `homeManager.flake` (both set → manifest validation error). Exactly one is
  the home-manager input.
- **Generate dynamically, persist inspectably.** The engine renders the flake per apply into a *stable*
  engine-state dir (e.g. `state/home-manager/<name>/flake.nix`), NOT a wiped temp — so it is inspectable
  and ejectable. Plain, commented `flake.nix`. Regenerated each run (the user's `home.nix` is the source of
  truth; generated output is not committed).
- **The generated flake** pins `nixpkgs` (`ENDSTATE_NIXPKGS_PIN`) and `home-manager`
  (`ENDSTATE_HOME_MANAGER_PIN`, `inputs.nixpkgs` follows), and exposes
  `homeConfigurations.<name> = home-manager.lib.homeManagerConfiguration { pkgs; modules = [ ./home.nix
  { home.username; home.homeDirectory; home.stateVersion; } ]; }`. The user's `home.nix` is copied into the
  generated dir and referenced relatively (`./home.nix`) — an absolute path is forbidden under pure flake
  evaluation; see the RESOLVED smoke verdict below.
- **Identity injected by the engine** — `home.username` = current user, `home.homeDirectory` = current home
  dir, `home.stateVersion` = a pinned default (overridable via env) — so the user's `home.nix` never
  hardcodes machine identity (the moat). home-manager errors loudly if `USER`/`HOME` mismatch, so the engine
  is the right owner of these.
- **Reuse #81 end to end.** The resulting flakeref `<generated-dir>#<name>` is passed to the existing
  `ActivateHome` — no new activation logic, classification, backup, or recording. The generation records the
  activated config exactly as the flake path does today.
- **Inspectability / moat.** `apply --dry-run` emits the generated flake path + a "would activate" event and
  activates nothing; the generated flake persists for reading and is a real flake (ejectable — a power user
  can `nix run home-manager -- switch --flake <that dir>` themselves). Raw Nix stays in `error.detail`.

## Design

### Manifest
`HomeManagerConfig` gains `Config string` (`json:"config,omitempty"`). Load/validate rejects a manifest that
sets both `Config` and `Flake`.

### Flake generation (`internal/realizer/nix`)
A pure generator: input (config name, identity, host system, pins) → rendered `flake.nix` written to the
state dir, with the user's `home.nix` copied in beside it (`./home.nix`); returns the `<dir>#<name>`
flakeref. The render is a string template, unit-tested by asserting the pins, the relative module path
(`./home.nix`), the host system, and the injected identity appear in the output. The engine resolves
identity (`user.Current()`, home dir) and pins (reuse `defaultPin` / `defaultHomePin`). See the RESOLVED
smoke verdict for why the config is copied in rather than referenced by absolute path.

### Pipeline (`runApplyRealizer` config stage)
- `mf.HomeManager.Config != ""` → resolve the path (relative to the manifest) → generate the flake → set the
  flakeref → call the existing `ActivateHome(flakeref)` → record as today.
- `mf.HomeManager.Flake != ""` → the existing #81 path, unchanged.
- `--dry-run` → generate + report the path + "would activate", skip activation.

### Inspectability
Generated flake is plain + commented in the stable state dir; the dry-run reveals it; the generation records
the activated config; raw Nix → `error.detail` only.

## Risks / Verification

- A `home.nix` may reference inputs the wrapper does not provide (only `nixpkgs` + `home-manager`) →
  activation fails with a classified error (raw in detail). Documented limitation; richer needs use
  `homeManager.flake` directly.
- `home.stateVersion` mismatch → home-manager warns (non-fatal; seen in the #81 smoke).
- Path resolution is relative to the manifest dir — tested.
- **Hermetic:** generator unit tests (template / pins / identity / module path); command tests (config branch
  generates + calls `ActivateHome` with the generated ref; `--dry-run` reveals + does not activate; the
  `config`/`flake` mutual exclusion). `GOOS=windows` build/vet clean; openspec strict.
- **Real-nix smoke** (throwaway `$HOME`): a tiny `home.nix` (e.g. `programs.git.userName`) referenced by
  `homeManager.config` → `apply --enable-restore` generates the flake → activates → the managed config
  reflects it; `--dry-run` reveals the generated path and activates nothing; the generated flake persists and
  is ejectable.

## Open question (smoke decides) — RESOLVED

The real-nix smoke (throwaway `$HOME`, `home.nix` = `programs.git.userName = "smoke"`, system `x86_64-linux`,
Determinate Nix 3.21.0) resolved both the activation mechanism and the `flake.lock` question:

- **flake.lock — VERDICT: do NOT engine-generate a lock; rely on the engine pins (matches #81).** A generated
  flake whose inputs are pinned via `ENDSTATE_NIXPKGS_PIN` / `ENDSTATE_HOME_MANAGER_PIN` activates cleanly.
  Nix itself writes a `flake.lock` into the persistent state dir on first activation (it is writable and not
  wiped), so within-machine reproducibility is automatic without the engine owning a lock; full cross-machine
  reproducibility is achieved exactly as in #81 — by setting the `ENDSTATE_*_PIN`s to rev-locked refs.
  Engine-generating a lock would require an extra impure `nix flake lock` invocation (the generator is a pure
  string template) and a lock would be clobbered by the per-run regeneration anyway. The flake we write
  (flake.nix + the copied home.nix) leaves any existing `flake.lock` in place.

- **Module reference — DEVIATION from the locked "absolute path" decision (the smoke forced it).** The smoke
  proved that referencing the user's `home.nix` by **absolute path** fails: `home-manager switch` evaluates the
  flake in **pure mode**, which raises `error: access to absolute path '<.../home.nix>' is forbidden in pure
  evaluation mode`. The spec requirement (generate a flake that wraps the config **and activates it**) is
  mandatory and outranks the mechanism detail. Resolution: the engine **copies the user's `home.nix` into the
  generated flake directory** and references it relatively as `./home.nix`. This activates cleanly (the managed
  `~/.config/git/config` reflected `userName = smoke`) and is strictly better for the *inspectable + ejectable*
  tenet — the generated directory is now a complete, self-contained flake a power user can `nix run
  home-manager -- switch --flake <that dir>` by hand. The contract, generator, and tests reflect `./home.nix`,
  not an absolute path.

### Generated flake shape (smoke-proven)
`pkgs` is pinned to the host system (e.g. `x86_64-linux`) derived from the Go runtime, avoiding an impure
`builtins.currentSystem`. Identity is injected as `home.username` / `home.homeDirectory` / `home.stateVersion`
(state version overridable via `ENDSTATE_HM_STATE_VERSION`, default `24.05`). The config attribute name and
state subdir are the current username.
