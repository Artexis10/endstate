## Why

The Nix realizer compiles `homeManager.settings` into a `home.nix`, wraps it in a generated,
pinned flake, and activates it (PRs #87/#91). That generated directory is deliberately
**inspectable and ejectable** and may be committed to a dotfiles repo. A user who needs secret
material in their home configuration — an SSH private key, an API token in
`home.sessionVariables`, a `~/.netrc`, an `.npmrc` auth line — has had nowhere to declare it that
does **not** write the plaintext into the generated, committable `home.nix` / staged `files/`.
Worse, `capture` re-emits `homeManager.settings`; a secret that reached `settings.files` or
`settings.shell.sessionVariables` would round-trip into the captured manifest in plaintext.

The design-only change `nix-home-manager-secrets-scope` ratified the direction:
**referenced, never embedded** — the engine NEVER holds secret material. This change is the
**Phase-1 implementation** of that direction: the documented-boundary backend (no new
dependency). The user provisions the secret out-of-band; the engine emits only a reference to
where it lands.

## What Changes

- **Typed input `homeManager.secrets`** — a list of `{name, path|env, backend?}` on
  `HomeManagerConfig`, a SIBLING of `Flake` / `Config` / `Settings` (composes with the
  engine-generated modes; NOT part of their mutual exclusivity). **Phase 1 is PATH-ONLY**: each
  entry declares a `path` reference; the `Env` field exists for a future phase but is rejected at
  load (in the boundary model the engine never holds a value, so it cannot set an env var).
  `backend` defaults to `"boundary"` (the only Phase-1 backend); any other value is rejected at load.
- **The reference sink** — a `path` entry → `home.file.<homeRelTarget>.source =
  config.lib.file.mkOutOfStoreSymlink <path>;` (symlinked at activation — never imported into the
  world-readable `/nix/store`, and pure-eval-safe). The references are emitted into a
  SEPARATE generated module (`secrets.nix`) staged beside the flake and added to the flake's
  `modules` list, so they compose uniformly with both `settings` and `config` modes without
  touching the user's `home.nix`.
- **No-embed BY CONSTRUCTION** — a secret entry NEVER enters the `staged map[string][]byte` that
  `writeHomeFlake` materializes and is NEVER `os.ReadFile`'d. The secret path/env is emitted as a
  Nix reference only; there is no code path from a secret entry to file content (unlike the
  catalog `files` map, which reads source content at compile time).
- **Capture = reference only** — `recoverHomeManager` carries the secret references
  (`path`/`env`/`backend`) through capture alongside the recovered `settings`/`config`; it never
  carries material (the references hold none).
- **Reject secrets with pure flake mode** — an external flake owns its own secrets; the engine
  generates nothing to inject reference sinks into, so `homeManager.secrets` combined with
  `homeManager.flake` is rejected at load.
- **Detect-and-warn on a missing key** — Phase 1 relies on load-time validation and on NOT
  swallowing activation errors; the engine NEVER generates or stores a key.

## Capabilities

### New Capabilities

- `nix-home-manager-secrets-boundary`: the Phase-1 documented-boundary implementation under which a
  user declares `homeManager.secrets` and the engine references — never embeds — that material in
  the generated, inspectable flake/home.nix and in any captured manifest. This capability is named
  distinctly from the design-only `nix-home-manager-secrets` (scope) capability so the two
  coexist; the scope change states the backend-agnostic invariants, this change states the
  concrete Phase-1 boundary behavior the engine implements.

### Modified Capabilities

- None. Secrets compose additively with the existing generated-flake and capture capabilities; no
  existing behavior changes when `homeManager.secrets` is absent.

## Impact

- `go-engine/internal/manifest/types.go` — `HomeManagerSecret` + `Secrets []HomeManagerSecret` on
  `HomeManagerConfig`.
- `go-engine/internal/manifest/validator.go` — load-time validation (path XOR env, unique
  non-empty name, supported backend, reject with flake mode).
- `go-engine/internal/realizer/nix/home_secrets.go` (new) — `compileSecretsModule`: the
  reference-only `secrets.nix` generator (the no-embed keystone).
- `go-engine/internal/realizer/nix/home_flake.go` / `home_catalog.go` — thread secrets through
  `writeHomeFlake` / the two public generators; the flake `modules` list gains `./secrets.nix`.
- `go-engine/internal/commands/apply_realizer.go` — pass `mf.HomeManager.Secrets` to generation;
  record the references on the provisioning generation.
- `go-engine/internal/provision/provision.go` — `HomeGenRef.Secrets` (references only).
- `go-engine/internal/commands/capture_realizer.go` — `recoverHomeManager` carries the references.
