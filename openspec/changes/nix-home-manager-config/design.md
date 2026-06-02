## Context

The Nix realizer (`internal/realizer/nix`) owns packages only: `Current`/`Plan`/`Realize`(+`Pruner`/
`Rollbacker`), via `nix profile`. The realizer `apply` path (`runApplyRealizer`) has plan/install/
prune/verify phases — **no config stage**. The Windows config layer (`modules/`+`internal/restore`)
is imperative file/registry plumbing keyed to `%ProgramFiles%`/winget — not reusable for Nix.
home-manager is the Nix-native config tool: `home-manager switch --flake <ref>#<name>` activates a
user's declarative config (dotfiles via `home.file`, typed settings via `programs.*`), creating a
numbered home-manager generation. There is no automated "machine → home.nix" capture (Nix community
guidance is manual migration), so this change is **apply/orchestration only**.

## Goals / Non-Goals

**Goals:** apply a user's home-manager config as the realizer's config stage; engine-owned
invocation (user installs nothing); record it in the Provisioning Generation; back up clobbered
files; zero Windows/default regression.

**Non-Goals (follow-ons):** home-manager rollback (smoke-gated fast-follow); config *capture*;
hiding the flake behind engine-generated `home.nix`/JSON (the catalog); nix-darwin/NixOS.

## Decisions (maintainer, brainstormed)

- **Start = orchestration core** (run switch + record), not capture or catalog.
- **Invocation = engine-owned `nix run home-manager/<pin> -- switch`** (no user install; pinned;
  preserves the moat) — not "assume home-manager installed".
- **Surface = a config stage in `apply`**, gated by the existing **`--enable-restore`** (the config
  gate; no weird new `--enable-home`) — not a separate command.
- **Input = `homeManager.flake` (flakeref passthrough)** as a permanent power-user escape hatch; the
  orchestrator is input-agnostic, so engine-generated inputs layer on later without rework.

## Design

### Manifest
`manifest.Manifest` gains `HomeManager *HomeManagerConfig` with `Flake string` (`json:"flake"`).
Absent ⇒ no config stage (default apply unchanged).

### Activation (engine-owned, realizer-only)
New `internal/realizer/nix/home_manager.go`: a function that runs, through the realizer's existing
runner (which already injects `--extra-experimental-features 'nix-command flakes'`):

`nix run <home-manager-pin> -- switch --flake <ref> -b endstate-backup`

- `<home-manager-pin>` defaults to a pinned `github:nix-community/home-manager/<rev|release>`,
  overridable via `ENDSTATE_HOME_MANAGER_PIN` (mirrors `ENDSTATE_NIXPKGS_PIN`).
- `-b endstate-backup` makes home-manager move any pre-existing file it would replace to
  `<file>.endstate-backup` instead of failing (honors *backup-before-overwrite*).
- Result classified through the existing `classify`/anchor path: spawn/daemon → `REALIZER_UNAVAILABLE`,
  permission → `PERMISSION_DENIED`, eval/activation error → `INSTALL_FAILED` (reuse), raw text →
  `error.detail` only. Returns the new home-manager generation number on success (read from
  `home-manager generations` or the switch output) for the record.

### Pipeline
In `runApplyRealizer`, after the prune/verify phases, a **config stage**: `if flags.EnableRestore &&
mf.HomeManager != nil && mf.HomeManager.Flake != ""` → activate. Emits config-stage item/summary
events consistent with the event contract. The winget/driver `apply` path is untouched.

### Generation record
`provision.Generation` gains optional `HomeManager *HomeGenRef{ Flake string; Generation int }`. The
config stage populates it; `writeProvisioningGeneration` carries it. Recorded even when no package
changed (a config-only apply is still a recorded provisioning event) — refine the write-gate so a
home-manager activation counts as "something happened".

### Safety / moat
Opt-in (`--enable-restore` + manifest field); backup-on-clobber; realizer-only; raw Nix/home-manager
text confined to `error.detail`. No Windows path, no default-apply change.

## Risks / Verification

- **home-manager behavior is the unknown** (like winget was): does `nix run home-manager -- switch
  --flake` activate cleanly here, what does it print, what's the generation number source, and
  (deferred) how does rollback re-activate a prior generation. Resolve with a **real-nix smoke** on
  this box using a **throwaway `$HOME`** (so activation can't touch the real user profile): a tiny
  `home.nix` setting e.g. `programs.git.userName`, `apply --enable-restore`, assert the generated
  `~/.gitconfig` + the recorded generation. The smoke decides whether rollback joins this change or
  becomes the fast-follow.
- Hermetic: inject the runner; assert the stage fires only with the flag + manifest field, passes
  the right argv, records the generation field, and no-ops otherwise. `GOOS=windows` build/vet clean
  (realizer path is non-Windows; winget path untouched). `go test ./...`; openspec strict.

## Smoke verdict (resolved — real Determinate Nix 3.21.0)

The home-manager unknown is resolved by a real-nix smoke on a throwaway `$HOME`. Findings:

- **Activation:** `nix run github:nix-community/home-manager -- switch --flake <ref> -b endstate-backup`
  activates cleanly (exit 0); the engine path (`apply --enable-restore`) reflects the flake in the managed
  file, moves a clobbered file to `<file>.endstate-backup`, and records the config. Without the flag the
  config is untouched (default apply byte-identical).
- **Generation-number source:** the home-manager **nix-profile symlink**
  `$XDG_STATE_HOME/nix/profiles/home-manager -> home-manager-<N>-link` — read identically to the package
  generation (no second nix invocation). The pin default is `github:nix-community/home-manager` (overridable
  via `ENDSTATE_HOME_MANAGER_PIN`).
- **Classification:** switch output is **plain text** (not nix internal-json), so classification anchors the
  raw stderr through the existing anchor table; the engine runner must place the experimental-features flag
  **before** the `nix run` `--` separator (the `nixArgs` fix), or it would be passed to home-manager.
- **Rollback → fast-follow (confirmed).** Mechanism is feasible — re-running a prior generation's
  `<store-path>/activate` (store path + id from `home-manager generations` / the `-<N>-link` symlink) exits
  0 — but it creates a NEW forward generation mirroring the old content rather than moving the pointer back
  (unlike `nix profile rollback --to`). That is a distinct mechanism from the package `Rollbacker`, so
  home-manager rollback ships as its own change with its own tests; this orchestration core de-risks it but
  does not include it.
