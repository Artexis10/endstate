## Context

The home-manager config arc: **(1) orchestration core** (#81 — `homeManager.flake` → engine-owned `switch`) → **(1b) config-file wrapper** (#87 — `homeManager.config`, a `home.nix` the engine wraps into a generated flake) → **(2) config capture** (#86/#89) → **(3) this catalog**. The wrapper deliberately built the generation seam (`home.nix` → `GenerateHomeFlake` → `<dir>#<name>` → `ActivateHome`) so a higher tier could "generate into" it. This change is that tier: the user declares config in Endstate's own format and the engine writes the `home.nix`.

It also brings the Unix config story to parity with Windows: the `module.jsonc` catalog restores arbitrary files (text/binary) + registry, not just structured settings. home-manager's `home.file` / `xdg.configFile` are the Unix analogue, so the catalog covers both structured settings and arbitrary files.

## Goals / Non-Goals

**Goals:** a declarative, Endstate-native config block (`homeManager.settings`) compiled to a `home.nix` and activated via the existing wrapper; a hybrid schema (curated concepts + raw `programs` passthrough); arbitrary-file placement (`files`); invisible by default, inspectable + ejectable; reuse #81/#87 unchanged.

**Non-Goals (deferred):** capture into the catalog; broad curated coverage; secrets-bearing programs; large editor surfaces; home-manager rollback.

## Decisions

- **Layer on the wrapper, do not duplicate it.** The catalog compiles `settings` → a `home.nix`, then calls the EXISTING `GenerateHomeFlake` / `ActivateHome` (#87/#81). Both artifacts (the generated `home.nix` AND the generated flake) persist and are inspectable. Rejected: generating the flake directly (duplicates #87) or making the user wire `homeManager.config` to the generated file (two manual steps).
- **Input = `homeManager.settings`** — an inline object. **Mutually exclusive** with `config` and `flake` (exactly one home-manager input; enforced at load, extending the wrapper's `config`-XOR-`flake` check to three-way).
- **Hybrid schema.** Curated, Endstate-native concepts (v1: `git`, `shell`, `direnv`, `starship`) are mapped by the engine to the correct home-manager options; an embedded `programs` object is passed through verbatim. The curated mapping is the moat: it insulates the user from home-manager option renames (the #87 smoke already saw `programs.git.userName` → `programs.git.settings.user.name`) — the engine owns that mapping so the user's `git.userName` never breaks.
- **`files` of all kinds.** `settings.files` maps a target (`~/.config/foo/bar` or an `xdg`-relative path) to a source path (resolved relative to the manifest). The engine COPIES each source into the generated flake dir (binary-safe; pure-eval forbids absolute paths outside the flake tree — the exact constraint #87's smoke surfaced) and emits `home.file."<target>".source = ./<staged>` (or `xdg.configFile`). Restore/place only in v1.
- **Curated keys are validated; raw `programs` is not.** Unknown curated keys (e.g. a typo under `git`) fail at load with a clear error; the raw `programs` block is passed through and any mistake surfaces as a classified activation error with raw Nix in `error.detail` (the moat).

## Design

### Manifest
`HomeManagerConfig` gains `Settings *HomeManagerSettings` (`json:"settings,omitempty"`). `HomeManagerSettings` = the curated concepts (typed) + `Programs map[string]any` (raw passthrough) + `Files map[string]string` (target → source). Load/validate rejects a manifest that sets more than one of `settings` / `config` / `flake`.

### Catalog compiler (`internal/realizer/nix`)
A pure compiler: `(settings, identity-free)` → `home.nix` text. Two cooperating pieces, both unit-testable:
- a **curated mapping table** — each concept → its home-manager option(s) (`git.userName` → the current `programs.git` option; `shell.aliases` → `home.shellAliases`; `shell.sessionVariables` → `home.sessionVariables`; `direnv`/`starship` → `programs.<x>`);
- a **JSON→Nix value encoder** — bools, numbers, strings (Nix-escaped), lists, and attrsets, for the raw `programs` passthrough.
`files` entries are staged (copied) into the flake dir by the writer step and rendered as `home.file`/`xdg.configFile` source references. Identity (`home.username`/`homeDirectory`/`stateVersion`) is still injected by `GenerateHomeFlake`, not the catalog.

### Pipeline (`runApplyRealizer` config stage)
`resolveHomeFlake` (from #87) gains a branch: `mf.HomeManager.Settings != nil` → compile to a `home.nix` (written into the generated flake dir alongside staged `files`) → `GenerateHomeFlake` → the `<dir>#<name>` flakeref → existing `ActivateHome`; record as today. `--dry-run` compiles + reveals the generated path, activates nothing. `config` and `flake` branches unchanged.

### Inspectability
The generated `home.nix`, the staged `files`, and the generated flake are all plain/readable in the stable state dir, persist after apply, and are revealed by `--dry-run`. Raw Nix → `error.detail` only.

## Risks / Verification

- A raw `programs` block can reference home-manager options the pinned version lacks → classified activation error (raw in detail). Documented; richer needs use `homeManager.config`/`flake`.
- `files` sources must exist at apply time and be readable → clear load/generation error if missing (mirrors the wrapper's missing-`home.nix` handling).
- **Hermetic:** unit tests for the curated mapping table, the JSON→Nix encoder, `files` staging, and the `settings` branch of `resolveHomeFlake` (reusing #87's `newRealizerFn`/`fakeRealizer` seams + the both-seams CI rule). `GOOS=windows` build/vet clean; openspec strict.
- **Real-nix smoke** (throwaway `$HOME`): a `homeManager.settings` block (`git.userName` curated + a raw `programs` entry + a `files` dotfile) → `apply --enable-restore` compiles the `home.nix` → generates the flake → activates → the managed git config + the placed file reflect the declaration; `--dry-run` reveals the generated `home.nix`/flake and activates nothing; both persist and are ejectable.

## Open questions (resolve during implementation)

- **Exact curated→home-manager option mapping per concept** — pin against the engine's pinned home-manager at implementation time (the #87 smoke showed these options move; the mapping table is the single place that absorbs it). Record the confirmed mappings the way #87/#81 recorded their empirical smoke verdicts.
- **`xdg.configFile` vs `home.file` selection for `files`** — whether to infer XDG targets or always use `home.file` with absolute targets; decide with the smoke.
