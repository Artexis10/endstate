## Context

- `manifest.HomeManagerSettings` (`internal/manifest/types.go`, PR #91) is the declarative catalog
  struct: typed curated fields (`Git`, `Shell`, `Direnv`, `Starship`) + a permissive `Programs` raw
  passthrough + a `Files` map. Its custom `UnmarshalJSON` decodes with `DisallowUnknownFields`, so any
  unknown sub-key WITHIN a typed curated concept fails to load. `Programs`/`Files` are maps and stay
  permissive (maps have no "unknown fields").
- `CompileHomeNix` (`internal/realizer/nix/home_catalog.go`) renders the `home.nix`: curated concepts
  become fixed `programs.*`/`home.*` statements, the raw `programs` block is forwarded verbatim
  (`nixValue`), and `files` are staged + placed via `home.file`. Output is deterministic (sorted keys).
- `curatedPrograms` lists each curated concept that owns a `programs.<name>` entry. A raw `programs`
  key matching one is rejected (`programs.<name> conflicts with the curated "<name>" concept`), because
  the curated concept already emits that `programs.<name>` and Nix would reject a double definition.

## Goals / Non-Goals

**Goals:**
- Add five curated concepts (`fzf`, `zoxide`, `bat`, `tmux`, `ssh`), each mapped to a STABLE
  home-manager surface, so the catalog covers a common developer shell without dropping to the raw
  passthrough.
- Preserve unknown-sub-key rejection, the raw-overlap conflict, and deterministic rendering.
- Hermetic tests only; no live Nix invocations.

**Non-Goals:**
- No change to the four existing curated concepts, the raw passthrough, or the files map.
- No capture-side change (a settings-applied machine already round-trips: capture records the whole
  `HomeManagerSettings`, which now simply carries more fields).
- No exhaustive home-manager option surface — only the stable, rename-insulated keys below.

## Decisions

- **Prefer stable surfaces, mirroring git → `programs.git.extraConfig`.** Curated concepts exist to
  insulate the declaration from home-manager option renames. `extraConfig`/`config`/`enable` are the
  long-lived, rarely-renamed surfaces of these modules; per-feature typed options (e.g.
  `programs.tmux.keyMode`) churn and would defeat the purpose. So `tmux`/`ssh` expose the raw config
  string, `bat` exposes the config attrset, and `fzf`/`zoxide` expose only `enable`.
- **Reuse `ProgramToggle` for `fzf`/`zoxide`** — they are pure enable flags, identical to
  `direnv`/`starship`. `bat`/`tmux`/`ssh` get small typed structs (`BatSettings`, `TmuxSettings`,
  `SSHSettings`) because they carry a second field; typed structs are what trigger the unknown-sub-key
  rejection.
- **Register all five in `curatedPrograms`** — each owns a `programs.<name>` entry, so a raw
  `programs.<name>` must conflict (reuse the existing error). Without this a user could double-define
  `programs.fzf` and get an opaque Nix error.
- **`enable` is rendered explicitly even when false** — like the existing direnv/starship toggles —
  so a user can pin a program OFF; the second field (config/extraConfig) is omitted when empty.

## Design

### Curated mapping table

| Endstate concept     | Stable home-manager option(s)                     | Typed struct      | Why stable |
|----------------------|---------------------------------------------------|-------------------|------------|
| `fzf.enable`         | `programs.fzf.enable`                              | `ProgramToggle`   | `enable` is the module's permanent on/off surface (like direnv/starship). |
| `zoxide.enable`      | `programs.zoxide.enable`                           | `ProgramToggle`   | Same — pure enable toggle. |
| `bat.enable`         | `programs.bat.enable`                              | `BatSettings`     | `enable` + the `config` attrset are bat's documented long-lived surface; `config` keys are bat's own config names, forwarded verbatim. |
| `bat.config`         | `programs.bat.config` (`key→value` attrset)       | `BatSettings`     | |
| `tmux.enable`        | `programs.tmux.enable`                             | `TmuxSettings`    | `extraConfig` is a raw `tmux.conf` string — tmux's own stable config language; insulates from home-manager per-option renames. |
| `tmux.extraConfig`   | `programs.tmux.extraConfig` (raw `tmux.conf`)     | `TmuxSettings`    | |
| `ssh.enable`         | `programs.ssh.enable`                              | `SSHSettings`     | `extraConfig` is a raw ssh-config string — ssh's own stable config language; same insulation rationale. |
| `ssh.extraConfig`    | `programs.ssh.extraConfig` (raw ssh config)       | `SSHSettings`     | |

### Schema (`internal/manifest/types.go`)

```go
type HomeManagerSettings struct {
    Git      *GitSettings   `json:"git,omitempty"`
    Shell    *ShellSettings `json:"shell,omitempty"`
    Direnv   *ProgramToggle `json:"direnv,omitempty"`
    Starship *ProgramToggle `json:"starship,omitempty"`
    Fzf      *ProgramToggle `json:"fzf,omitempty"`     // NEW
    Zoxide   *ProgramToggle `json:"zoxide,omitempty"`  // NEW
    Bat      *BatSettings   `json:"bat,omitempty"`     // NEW
    Tmux     *TmuxSettings  `json:"tmux,omitempty"`    // NEW
    SSH      *SSHSettings   `json:"ssh,omitempty"`     // NEW
    Programs map[string]any    `json:"programs,omitempty"`
    Files    map[string]string `json:"files,omitempty"`
}

type BatSettings struct {
    Enable bool              `json:"enable,omitempty"`
    Config map[string]string `json:"config,omitempty"`
}
type TmuxSettings struct {
    Enable      bool   `json:"enable,omitempty"`
    ExtraConfig string `json:"extraConfig,omitempty"`
}
type SSHSettings struct {
    Enable      bool   `json:"enable,omitempty"`
    ExtraConfig string `json:"extraConfig,omitempty"`
}
```

### Rendering (`internal/realizer/nix/home_catalog.go`)

`curatedPrograms` gains `fzf`, `zoxide`, `bat`, `tmux`, `ssh`. `CompileHomeNix` appends, after the
direnv/starship blocks:

```go
if s.Fzf != nil    { stmts = append(stmts, "programs.fzf.enable = "+nixValue(s.Fzf.Enable)+";") }
if s.Zoxide != nil { stmts = append(stmts, "programs.zoxide.enable = "+nixValue(s.Zoxide.Enable)+";") }
if s.Bat != nil {
    stmts = append(stmts, "programs.bat.enable = "+nixValue(s.Bat.Enable)+";")
    if len(s.Bat.Config) > 0 {
        stmts = append(stmts, "programs.bat.config = "+nixValue(stringMapToAny(s.Bat.Config))+";")
    }
}
if s.Tmux != nil {
    stmts = append(stmts, "programs.tmux.enable = "+nixValue(s.Tmux.Enable)+";")
    if s.Tmux.ExtraConfig != "" {
        stmts = append(stmts, "programs.tmux.extraConfig = "+nixValue(s.Tmux.ExtraConfig)+";")
    }
}
if s.SSH != nil {
    stmts = append(stmts, "programs.ssh.enable = "+nixValue(s.SSH.Enable)+";")
    if s.SSH.ExtraConfig != "" {
        stmts = append(stmts, "programs.ssh.extraConfig = "+nixValue(s.SSH.ExtraConfig)+";")
    }
}
```

`nixValue` already escapes newlines/quotes/`${`, so the raw `extraConfig` strings render as correct,
single-line Nix string literals.

## Risks / Verification

- **Two pre-existing tests used `bat`/`fzf` as raw-passthrough placeholders.** Now that those names
  are curated, a raw `programs.bat` is a conflict — correct behavior. The tests switch to a non-curated
  name (`htop`/`lsd`), preserving their intent (raw passthrough of an arbitrary program).
- **Hermetic tests** — render each new concept; explicit-false toggle; raw-overlap conflict for all
  five; unknown-sub-key rejection (`bat.confgi`, `tmux.extraConfigg`, `ssh.extarConfig`, `fzf.enabel`);
  determinism unchanged.
- **`GOOS=windows` build + `go vet`** — the catalog path is Nix-only; Windows never reaches it.
- **Real-nix smoke** (Linux dev box): apply a manifest declaring 2-3 new concepts with
  `--enable-restore`, then assert the activated home-manager config exposes them (e.g. `bat config`,
  `~/.tmux.conf`, `~/.ssh/config`).
