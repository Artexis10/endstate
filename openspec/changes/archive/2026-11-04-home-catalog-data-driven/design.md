## Context

- `manifest.HomeManagerSettings` (`internal/manifest/types.go`) is the declarative catalog struct:
  typed curated fields + a permissive `Programs` raw passthrough + a `Files` map. Its custom
  `UnmarshalJSON` decodes with `DisallowUnknownFields`, so a mistyped sub-key WITHIN a typed
  curated concept fails to load (the moat). `Programs`/`Files` are maps and stay permissive.
- `CompileHomeNix` (`internal/realizer/nix/home_catalog.go`) renders the `home.nix`: curated
  concepts become fixed `programs.*`/`home.*` statements, the raw `programs` block is forwarded
  verbatim (`nixValue`), `files` are staged + placed via `home.file`. Output is deterministic
  (sorted keys/targets).
- `curatedPrograms` lists each curated concept that owns a `programs.<name>` entry. A raw
  `programs` key matching one is rejected (`programs.<name> conflicts with the curated ...`).
- Before this change, every curated concept had its own bespoke emit block. The 13 blocks share
  one shape: `programs.<name>.enable = <bool>;` then, when non-empty, a single STABLE second
  field rendered per its kind (raw string, key→value attrset, nested attrset, or a list).

## Goals / Non-Goals

**Goals:**
- Collapse the per-concept emit blocks into ONE descriptor table + ONE generic emit loop, so the
  marginal cost of a new curated program is one typed field + one table row (no new emit branch).
- Keep the generated `home.nix` byte-identical for every existing concept (behavior-preserving).
- Add 11 dotfiles/CLI-tier concepts, each mapped to a STABLE home-manager surface.
- Preserve unknown-sub-key rejection, the raw-overlap conflict, deterministic rendering.
- Hermetic tests only; no live Nix invocations (Windows dev box cannot run home-manager).

**Non-Goals:**
- No change to `git` / `shell` handling — they are genuinely non-uniform and stay bespoke.
- No change to the raw passthrough, the files map, or the secrets sibling.
- No capture-side change (a settings-applied machine round-trips via `HomeManagerSettings`;
  `recoverHomeManager` recovers from provisioning history, not disk introspection — deferred).
- No exhaustive home-manager option surface — only the stable, rename-insulated keys below.

## Decisions

- **`fieldKind` enum + descriptor table.** A curated concept's optional second field is one of
  five kinds: `kindNone` (bare `.enable`, e.g. fzf), `kindString` (raw string → `extraConfig`/
  `initContent`/`initExtra`), `kindStringMap` (`map[string]string` → attrset, e.g. `bat.config`),
  `kindAnyMap` (`map[string]any` → nested attrset, e.g. `gh.settings`), `kindStringSlice`
  (`[]string` → Nix list, e.g. `eza.extraOptions`). One `curatedProgram{Name, StableField, Kind,
  get}` row per concept; `get` returns `(present, enable, second any)` so the loop never reflects.

- **Table emission order == output order.** The loop walks `curatedTable` in declared order and
  appends statements, exactly matching the prior block order (direnv → starship → fzf → zoxide →
  bat → tmux → ssh → eza → gh → lazygit → neovim, then the new dotfiles tier). `git` is emitted
  before the loop and `shell` between git and the loop — preserving the prior overall order so
  existing `strings.Contains` golden assertions and determinism are unaffected.

- **`curatedPrograms` derived from the table.** `buildCuratedPrograms()` seeds `{git: true}`
  (the only program-owning bespoke concept; shell maps to `home.*`) and adds every table `Name`.
  A new row therefore registers its raw-overlap guard automatically — no second place to edit.

- **`enable` rendered explicitly even when false** — unchanged policy (user can pin OFF). The
  optional second field is omitted when empty (`secondFieldEmpty` per kind).

- **Stable surfaces for the 11 new concepts** (rename-insulation is the whole point):
  - `zsh.initContent` over `initExtra` — home-manager consolidated the zsh init options
    (`initExtra`/`initExtraFirst`/`initExtraBeforeCompInit`) into a single `initContent` with
    `mkOrder`; `initContent` is the current canonical, rename-proof surface.
  - `bash.initExtra` — the long-stable bash init surface.
  - `ripgrep.arguments` / `fd.extraOptions` — each tool's own raw CLI-flag namespace (`listOf
    str`), stable because it mirrors the tool's flags, not a home-manager per-feature option.
  - `helix`/`kitty`/`alacritty`/`jujutsu`/`atuin`/`yazi` `.settings` — each forwarded verbatim to
    the tool's own config format (config.toml / kitty.conf / alacritty.toml / yazi.toml). Stable
    because it mirrors the upstream tool config directly; `map[string]any` supports nesting.
  - `wezterm.extraConfig` — raw Lua string, the documented stable surface (like tmux/ssh/neovim).

## Design

### Descriptor

```go
type fieldKind int
const ( kindNone fieldKind = iota; kindString; kindStringMap; kindAnyMap; kindStringSlice )

type curatedProgram struct {
    Name        string    // programs.<Name>; also the curated/raw-overlap key
    StableField string    // rename-proof second option ("" ⇒ none)
    Kind        fieldKind
    get         func(s *manifest.HomeManagerSettings) (present, enable bool, second any)
}
var curatedTable = []curatedProgram{ /* one row per uniform concept */ }
```

### Generic emit loop (replaces the per-concept blocks)

```go
for _, c := range curatedTable {
    present, enable, second := c.get(s)
    if !present { continue }
    stmts = append(stmts, "programs."+c.Name+".enable = "+nixValue(enable)+";")
    if c.StableField != "" && !secondFieldEmpty(c.Kind, second) {
        stmts = append(stmts, "programs."+c.Name+"."+c.StableField+" = "+renderSecondField(c.Kind, second)+";")
    }
}
```

### New curated mapping table (the 11)

| Endstate concept       | Stable home-manager option(s)                  | Kind          |
|------------------------|------------------------------------------------|---------------|
| `ripgrep`              | `programs.ripgrep.arguments` (`listOf str`)    | stringSlice   |
| `fd`                   | `programs.fd.extraOptions` (`listOf str`)      | stringSlice   |
| `zsh`                  | `programs.zsh.initContent` (str)               | string        |
| `bash`                 | `programs.bash.initExtra` (str)                | string        |
| `helix`                | `programs.helix.settings` (attrset)            | anyMap        |
| `kitty`                | `programs.kitty.settings` (attrset)            | anyMap        |
| `alacritty`            | `programs.alacritty.settings` (attrset)        | anyMap        |
| `wezterm`              | `programs.wezterm.extraConfig` (Lua str)       | string        |
| `jujutsu`              | `programs.jujutsu.settings` (attrset)          | anyMap        |
| `atuin`                | `programs.atuin.settings` (attrset)            | anyMap        |
| `yazi`                 | `programs.yazi.settings` (attrset)             | anyMap        |

## Risks / Verification

- **Behavior preservation** — the existing golden tests (`TestCompileHomeNix_CuratedAndRaw`,
  `_BroadenedCurated`, `_MoreCurated`, `_RawProgramOverlapErrors`, `_StagesFiles`, …) must stay
  green unchanged; they assert the byte-level statements the table now produces.
- **Hermetic tests** — render each new concept; raw-overlap conflict for all 11; unknown-sub-key
  rejection; enable-only (no empty second field) for a representative concept; determinism.
- **`go vet` + Windows build** — the catalog path is Nix-only; Windows never reaches it.
- **Real-nix activation smoke is a PENDING WSL follow-up** — `home-manager switch` cannot run on
  the Windows dev box. One smoke per StableField KIND (string / stringMap / anyMap / stringSlice
  / none) is owed on a Linux/WSL box before the gate; tracked in tasks.md.
