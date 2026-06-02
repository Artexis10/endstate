## Why

Phases 1–7 gave the Nix realizer the full **package** loop on Linux/macOS (install, generation,
rollback, prune, version). But Endstate has a second concern — **config** (settings/dotfiles) — and
on Nix it is entirely absent: the realizer `apply` path is package-only, and the existing config
layer (`modules/`+`restore/`) is Windows-oriented (winget matches, `%ProgramFiles%`, registry, copy/
merge-ini). Meanwhile Nix's signature config capability, **home-manager** (declarative dotfiles +
typed program settings), is exactly the "settings management" that can make the Linux/macOS side
*richer* than Windows.

This change is the **first step** toward Nix config management: the **home-manager orchestration
core**. Endstate applies a user's home-manager config as a config stage of `apply`, records it, and
keeps the **engine the lifecycle owner** — the user never installs or learns home-manager (the same
"moat" the package realizer preserves). It is deliberately the smallest piece — orchestration only.
Capturing config from a machine, and a curated declarative catalog, are explicit follow-ons.

## What Changes

- New manifest field **`homeManager.flake`** — a home-manager flakeref (e.g.
  `/home/me/dotfiles#hugo` or `github:me/dotfiles#hugo`) pointing Endstate at a real home-manager
  flake to activate. This is a **power-user escape hatch** (parallel to a power user pinning a raw
  per-app nix ref); hiding it behind engine-generated config (a `home.nix` wrapper, or JSON settings
  → the `programs.*` catalog) is future work. All such inputs ultimately produce a flakeref this
  stage consumes, so the orchestrator is input-agnostic.
- New **config stage in the realizer apply path** (`runApplyRealizer`), after the package phases,
  gated by the existing **`--enable-restore`** flag: when the manifest declares `homeManager.flake`,
  Endstate runs an **engine-owned** `nix run home-manager/<pin> -- switch --flake <ref> -b <ext>` to
  activate the config. The home-manager version is **pinned by the engine** (the user installs
  nothing); `-b` makes home-manager **back up** any file it would clobber (honors
  *backup-before-overwrite*).
- The applied config is recorded in the **Provisioning Generation** (the flakeref + the resulting
  home-manager generation number), so config is part of the audit trail alongside packages.
- **Realizer-only.** The winget/driver path is untouched (Windows config = the restore-module
  layer). Without `homeManager.flake` or without `--enable-restore`, behavior is byte-identical. Raw
  home-manager/Nix failure text is confined to `error.detail` via the realizer's existing
  classification approach (the moat).

## Capabilities

### New Capabilities

- `nix-home-manager-config`: `apply --enable-restore` on the Nix realizer activates a declared
  home-manager config (`homeManager.flake`) via an engine-owned `home-manager switch`, backing up
  clobbered files and recording the applied config in the Provisioning Generation. Realizer-only and
  opt-in; the winget path and a default (no-config) apply are unaffected.

### Modified Capabilities

- None — additive. *Backup-before-overwrite* is honored via home-manager's `-b`; *separation of
  concerns* is preserved (this is the config stage, distinct from package install).

## Impact

- `internal/manifest/types.go` — add `HomeManager *HomeManagerConfig{ Flake string }` to the manifest.
- `internal/realizer/nix/` (new home_manager.go) — engine-owned `nix run home-manager -- switch`
  activation; classify failures via the existing anchor path; pin overridable via env.
- `internal/commands/apply_realizer.go` — a config stage after the package phases, gated by
  `flags.EnableRestore`, when the manifest declares `homeManager`; record in the generation.
- `internal/provision/provision.go` — optional `HomeManager` field on the `Generation` record.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (maintainer-approved, additive)**: document
  `homeManager.flake` + the config stage.
- **Zero Windows/default regression.** Proven by host-aware tests + `GOOS=windows` build/vet; a
  **real-nix home-manager smoke** on this box validates the actual `switch` behavior.

## Non-Goals (explicit follow-ons)

- **Rollback of the home-manager config.** home-manager keeps its own numbered generations, but the
  rollback mechanics are an empirical unknown — rollback ships as a **fast-follow** once a real-nix
  smoke confirms how to re-activate a prior home-manager generation (same discipline as the winget
  `--force` unknown).
- **Config capture** (machine → home-manager config) — only raw-file capture is automatable; a
  separate sub-project.
- **Hiding the flake** — engine-generated `home.nix` / a `programs.*` declarative catalog (the
  "richer than Windows" end-state); a separate sub-project that produces a flakeref this same stage
  consumes.
- **nix-darwin / NixOS system config** — user-scoped home-manager only.
