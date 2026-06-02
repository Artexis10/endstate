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
  `homeConfigurations.<name> = home-manager.lib.homeManagerConfiguration { pkgs; modules = [ <abs path to the
  user's home.nix> { home.username; home.homeDirectory; home.stateVersion; } ]; }`.
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
A pure generator: input (`home.nix` absolute path, config name, identity, pins) → rendered `flake.nix`
written to the state dir; returns the `<dir>#<name>` flakeref. The render is a string template, unit-tested
by asserting the pins, the user module path, and the injected identity appear in the output. The engine
resolves identity (`user.Current()`, home dir) and pins (reuse `defaultPin` / `defaultHomePin`).

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

## Open question (smoke decides)

- Whether to also generate a `flake.lock` (full reproducibility) or rely on the engine pins alone (the #81
  smoke activated cleanly with no lock). Resolve with the smoke and record the verdict, the way the
  home-manager generation-source and rollback questions were resolved for #81.
