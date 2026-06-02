## Context

- #87 `resolveHomeFlake` (`apply_realizer.go`): a `homeManager.config` is wrapped by
  `nix.GenerateHomeFlake(state.StateDir(), cfgPath)` into `state/home-manager/<user>/` and the
  generated `<dir>#<user>` flakeref is returned (`generated=true`); a `homeManager.flake` is returned
  verbatim (`generated=false`). Apply records `provision.HomeGenRef{Flake: <activated>, Generation}`.
- #86 `recoverHomeManager` (`capture_realizer.go`): reads `provision.List()` (newest-first) and emits
  `manifest.HomeManagerConfig{Flake: g.HomeManager.Flake}` from the most-recent generation whose
  `HomeManager` is non-nil.
- `manifest.HomeManagerConfig` (post-#87) = `{Flake, Config}`, mutually exclusive, both omitempty.

The gap: for a `config` apply, `HomeGenRef.Flake` is the generated, machine-local, ephemeral path, so
capture emits a non-portable `homeManager.flake`.

## Goals / Non-Goals

**Goals:** capture round-trips a `homeManager.config`-applied machine to its `home.nix`; preserve the
`homeManager.flake` round-trip; no regression elsewhere.

**Non-Goals:** capturing `home.nix` *content* (still out of scope — `config` is a path the user owns,
captured as a pointer, same portability caveat as a local flake path); changing the apply wrapper or
the generated-flake mechanism.

## Decisions

- **Record the declared input, keep the activated one for audit.** `HomeGenRef` gains `Config`. For a
  config apply, record `Config = mf.HomeManager.Config` (the value as declared) AND `Flake = <generated
  ref>` (what was actually activated — audit). For a flake apply, `Config` stays empty. This keeps the
  generation an accurate activation record while letting capture recover the user's intent.
- **Capture prefers `Config`.** `recoverHomeManager` emits `{Config}` when the selected generation has
  one, else `{Flake}`. Mirrors `homeManager`'s flake/config mutual exclusivity.
- **Record the declared value verbatim** (not the manifest-resolved absolute path), so capture
  re-emits exactly what the user wrote — symmetric with how the flake case re-emits the user's flake.

## Design

`provision.HomeGenRef`:

```go
type HomeGenRef struct {
    Flake      string `json:"flake"`
    Config     string `json:"config,omitempty"` // original homeManager.config (config-derived applies)
    Generation int    `json:"generation"`
}
```

`apply_realizer.go` config stage (where `homeRef` is built):

```go
homeRef = &provision.HomeGenRef{Flake: flake, Generation: hmGen}
if generated { // generated == true ⇒ flake came from mf.HomeManager.Config
    homeRef.Config = mf.HomeManager.Config
}
```

`capture_realizer.go` `recoverHomeManager`:

```go
for _, g := range gens { // newest-first
    if g.HomeManager == nil {
        continue
    }
    if g.HomeManager.Config != "" {
        return &manifest.HomeManagerConfig{Config: g.HomeManager.Config}
    }
    if g.HomeManager.Flake != "" {
        return &manifest.HomeManagerConfig{Flake: g.HomeManager.Flake}
    }
}
```

The `--update` fallback (preserve an existing manifest's `HomeManager`) is unchanged.

## Risks / Verification

- **Hermetic:** apply test (config path → `GenerateHomeFlake`, which is pure-fs, no nix) asserts the
  generation records `Config`; capture tests (inject `listGenerationsFn`) assert `homeManager.config`
  is emitted for a config generation and `homeManager.flake` for a flake generation.
- **Real-nix smoke:** apply via `homeManager.config` (a tiny `home.nix`, sandboxed `$HOME`, local hm
  pin) → capture → assert the manifest carries `homeManager.config` (not the `state/` flake path) →
  apply the captured manifest re-activates.
- **No regression:** flake-declared applies/captures unchanged; `go test ./...` + `GOOS=windows`
  build/vet green.
