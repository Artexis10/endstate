## Context

- `manifest.HomeManagerSettings` (`internal/manifest/types.go`) is the declarative catalog struct:
  typed curated fields + a permissive `Programs` raw passthrough + a `Files` map. Its custom
  `UnmarshalJSON` decodes with `DisallowUnknownFields`, so a mistyped sub-key WITHIN a typed curated
  concept fails to load. `Programs`/`Files` are maps and stay permissive (maps have no "unknown fields").
- `CompileHomeNix` (`internal/realizer/nix/home_catalog.go`) renders the `home.nix`: curated concepts
  become fixed `programs.*`/`home.*` statements, the raw `programs` block is forwarded verbatim
  (`nixValue`), and `files` are staged + placed via `home.file`. Output is deterministic (sorted keys).
- `curatedPrograms` lists each curated concept that owns a `programs.<name>` entry. A raw `programs`
  key matching one is rejected (`programs.<name> conflicts with the curated "<name>" concept`).

## Goals / Non-Goals

**Goals:**
- Add four curated concepts (`eza`, `gh`, `lazygit`, `neovim`), each mapped to a STABLE home-manager
  surface, so the catalog covers more of a common developer shell without dropping to the raw passthrough.
- Preserve unknown-sub-key rejection, the raw-overlap conflict, and deterministic rendering.
- Hermetic tests only; no live Nix invocations.

**Non-Goals:**
- No change to the nine existing curated concepts, the raw passthrough, or the files map.
- No capture-side change (a settings-applied machine already round-trips via `HomeManagerSettings`).
- No exhaustive home-manager option surface — only the stable, rename-insulated keys below.

## Decisions

- **`eza.extraOptions` as `[]string`** — `programs.eza.extraOptions` is a `listOf str` in home-manager;
  exposing it as a string slice keeps the surface idiomatic in JSON and avoids escaping. A new
  `stringSliceToAny` helper (mirroring `stringMapToAny`) lifts it to `[]any` for `nixValue` which
  renders it as `[ "--git" "--icons" ]`.
- **`gh.settings` and `lazygit.settings` as `map[string]any`** (not `map[string]string`) — lazygit's
  config is a deeply-nested YAML structure (e.g. `gui.theme.activeBorderColor`). `map[string]any`
  supports arbitrary nesting; `nixValue` already handles nested `map[string]any` as attrsets
  recursively. `gh` uses the same type for consistency even though its config is shallower.
- **`neovim.extraConfig` as raw string** — mirrors `tmux`/`ssh`. `programs.neovim.extraConfig` accepts
  both vimscript and Lua; it is the documented stable surface for arbitrary neovim configuration. The
  engine simply passes the string through `nixValue` (which escapes newlines, quotes, `${`).
- **Register all four in `curatedPrograms`** — each owns a `programs.<name>` entry; a raw
  `programs.<name>` must conflict for the same reason as the existing nine.
- **`enable` rendered explicitly even when false** — same policy as existing curated concepts (user can
  pin OFF). Optional second fields (`extraOptions`/`settings`/`extraConfig`) are omitted when empty.

## Design

### Curated mapping table

| Endstate concept         | Stable home-manager option(s)                          | Typed struct       | Why stable |
|--------------------------|--------------------------------------------------------|--------------------|------------|
| `eza.enable`             | `programs.eza.enable`                                  | `EzaSettings`      | `enable` is the permanent on/off surface. |
| `eza.extraOptions`       | `programs.eza.extraOptions` (`listOf str`)             | `EzaSettings`      | Raw CLI flags forwarded verbatim; eza's own flag namespace. |
| `gh.enable`              | `programs.gh.enable`                                   | `GhSettings`       | `enable` is permanent on/off. |
| `gh.settings`            | `programs.gh.settings` (attrset passthrough)           | `GhSettings`       | gh's own config key namespace; mirrors the gh CLI config format directly. |
| `lazygit.enable`         | `programs.lazygit.enable`                              | `LazygitSettings`  | `enable` is permanent on/off. |
| `lazygit.settings`       | `programs.lazygit.settings` (attrset passthrough)      | `LazygitSettings`  | lazygit's own config structure; raw passthrough avoids binding to renamed home-manager sub-options. |
| `neovim.enable`          | `programs.neovim.enable`                               | `NeovimSettings`   | `enable` is permanent on/off. |
| `neovim.extraConfig`     | `programs.neovim.extraConfig` (raw vimscript/lua)      | `NeovimSettings`   | `extraConfig` is the documented, stable surface (like `tmux`/`ssh`); insulates from per-option renames. |

### Schema additions (`internal/manifest/types.go`)

```go
// HomeManagerSettings gains four new fields:
Eza     *EzaSettings     `json:"eza,omitempty"`
Gh      *GhSettings      `json:"gh,omitempty"`
Lazygit *LazygitSettings `json:"lazygit,omitempty"`
Neovim  *NeovimSettings  `json:"neovim,omitempty"`

type EzaSettings struct {
    Enable       bool     `json:"enable,omitempty"`
    ExtraOptions []string `json:"extraOptions,omitempty"`
}
type GhSettings struct {
    Enable   bool           `json:"enable,omitempty"`
    Settings map[string]any `json:"settings,omitempty"`
}
type LazygitSettings struct {
    Enable   bool           `json:"enable,omitempty"`
    Settings map[string]any `json:"settings,omitempty"`
}
type NeovimSettings struct {
    Enable      bool   `json:"enable,omitempty"`
    ExtraConfig string `json:"extraConfig,omitempty"`
}
```

### Rendering additions (`internal/realizer/nix/home_catalog.go`)

`curatedPrograms` gains `eza`, `gh`, `lazygit`, `neovim`. `CompileHomeNix` appends, after the ssh block:

```go
if s.Eza != nil {
    stmts = append(stmts, "programs.eza.enable = "+nixValue(s.Eza.Enable)+";")
    if len(s.Eza.ExtraOptions) > 0 {
        stmts = append(stmts, "programs.eza.extraOptions = "+nixValue(stringSliceToAny(s.Eza.ExtraOptions))+";")
    }
}
if s.Gh != nil {
    stmts = append(stmts, "programs.gh.enable = "+nixValue(s.Gh.Enable)+";")
    if len(s.Gh.Settings) > 0 {
        stmts = append(stmts, "programs.gh.settings = "+nixValue(s.Gh.Settings)+";")
    }
}
if s.Lazygit != nil {
    stmts = append(stmts, "programs.lazygit.enable = "+nixValue(s.Lazygit.Enable)+";")
    if len(s.Lazygit.Settings) > 0 {
        stmts = append(stmts, "programs.lazygit.settings = "+nixValue(s.Lazygit.Settings)+";")
    }
}
if s.Neovim != nil {
    stmts = append(stmts, "programs.neovim.enable = "+nixValue(s.Neovim.Enable)+";")
    if s.Neovim.ExtraConfig != "" {
        stmts = append(stmts, "programs.neovim.extraConfig = "+nixValue(s.Neovim.ExtraConfig)+";")
    }
}
```

`stringSliceToAny` helper (next to `stringMapToAny`):
```go
func stringSliceToAny(s []string) []any {
    out := make([]any, len(s))
    for i, v := range s { out[i] = v }
    return out
}
```

## Risks / Verification

- **Hermetic tests** — render each new concept; raw-overlap conflict for all four; unknown-sub-key
  rejection (`eza.enabel`, `gh.settigns`, `lazygit.settigns`, `neovim.extraCfg`); determinism unchanged.
- **`GOOS=windows` build + `go vet`** — the catalog path is Nix-only; Windows never reaches it.
- **Real-nix smoke** (Linux dev box): apply a manifest declaring 2-3 new concepts with
  `--enable-restore`, assert the generated `home.nix` contains the expected statements, and assert
  home-manager activation succeeds (e.g. `eza` binary appears on PATH, `nvim` binary exists).
