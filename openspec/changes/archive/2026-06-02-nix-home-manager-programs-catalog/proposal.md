## Why

The home-manager config wrapper (`nix-home-manager-config-wrapper`, #87) lets a user supply a `home.nix` and the engine generates the surrounding flake. That still asks the user to write home-manager configuration in Nix. This change adds the **zero-Nix tier**: the user declares configuration in **Endstate's own format** and the engine writes the `home.nix` for them — which then flows through the existing wrapper (`home.nix` → generated flake → `ActivateHome`) unchanged. The user learns Endstate, not Nix.

It is also the point where the Unix config story reaches **parity with the Windows config story**. On Windows the module catalog (`module.jsonc`) restores configuration of *all kinds* — structured settings, registry, and arbitrary files (text and binary). The home-manager `programs.*` surface covers structured settings; this change adds an Endstate-native catalog for those settings **plus** a `files` concept that places arbitrary files (text or binary) via home-manager, so declaring config on Linux/macOS is as complete as on Windows.

## What Changes

- New manifest input **`homeManager.settings`** — an inline, declarative Endstate configuration block. **Mutually exclusive** with `homeManager.config` (a `home.nix` path, #87) and `homeManager.flake` (a flakeref, #81): exactly one home-manager input.
- **Hybrid schema** — a curated, Endstate-native set of high-level concepts (v1: `git`, `shell`, `direnv`, `starship`) that the engine maps to the correct home-manager options, **plus** a raw `programs` escape hatch passed through verbatim to home-manager. The curated layer also **insulates the user from home-manager option churn** (e.g. `git.userName` keeps working even as home-manager renames its underlying option).
- **`files` concept** — declare `target → source` (source resolved relative to the manifest); the engine stages each file beside the generated flake (binary-safe, pure-eval-safe — the same copy-in the wrapper uses for `home.nix`) and emits `home.file` / `xdg.configFile`. Files of all kinds, matching the Windows restore catalog. **Place/restore only**; capture into the catalog is out of scope.
- **Generation** — the engine compiles `settings` into a `home.nix` and feeds it to the **existing** `GenerateHomeFlake` / `ActivateHome` (#87); no new activation, classification, backup, or recording. The generated `home.nix` and flake stay inspectable + ejectable; `apply --dry-run` reveals both without activating.

## Capabilities

### Modified Capabilities

- `nix-home-manager-config`: adds a **declarative catalog input** the engine compiles into a `home.nix` (curated concepts + raw `programs` passthrough + arbitrary-`files` placement), reusing the wrapper's generation and the #81 activation unchanged. Additive — `homeManager.config`, `homeManager.flake`, and a default (no-config) apply are unchanged.

## Impact

- `internal/manifest/types.go` — add `Settings` to `HomeManagerConfig`; extend mutual-exclusion validation to `settings` / `config` / `flake` (exactly one).
- `internal/realizer/nix/` — a catalog compiler: (`settings` block) → rendered `home.nix` (curated mapping table + a JSON→Nix value encoder for the raw passthrough) + staged `files`; reuses `GenerateHomeFlake`.
- `internal/commands/apply_realizer.go` — `resolveHomeFlake` gains a `settings` branch: compile → generate flake → existing `ActivateHome`; `--dry-run` reveal.
- `docs/contracts/cli-json-contract.md` — **PROTECTED (additive)**: document `homeManager.settings`, the curated/raw hybrid, the `files` concept, the three-way mutual exclusion, and the dry-run reveal.

## Non-Goals (deferred)

- **Capture into the catalog** (machine → `settings`) — the harder direction; a separate sub-project (config capture is parked, per the wrapper's framing).
- **Broad curated coverage** — v1 is a deliberately small curated set (`git`, `shell`, `direnv`, `starship`) that proves the pipeline; expanding the catalog is a follow-on.
- **Secrets-bearing programs** (ssh keys, credentials) and **large editor surfaces** (neovim/emacs config trees) — out of v1.
- **home-manager rollback** — still the documented #81 follow-on.
