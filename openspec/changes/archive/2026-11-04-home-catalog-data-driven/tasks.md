> TDD: write each test RED first, then implement to green. Hermetic (no live Nix calls).
> Verify: `cd go-engine && go test ./internal/realizer/... ./internal/manifest/...` +
> `go vet ./...` + `GOOS=windows go build ./...`. Real-nix activation smoke is a PENDING WSL
> follow-up (see 4.4) — it cannot run on the Windows dev box.

## 1. The gate — data-driven emission (behavior-preserving)

- [x] 1.1 `internal/realizer/nix/home_catalog.go`: add `fieldKind` enum + `curatedProgram`
      descriptor + `curatedTable` (one row per uniform concept) + `secondFieldEmpty` /
      `renderSecondField` helpers; derive `curatedPrograms` from the table via
      `buildCuratedPrograms()` (seed `{git: true}`).
- [x] 1.2 Replace the per-concept emit blocks in `CompileHomeNix` with one generic loop over
      `curatedTable`. Keep `git` (nested user/init) and `shell` (`home.*`) bespoke.
- [x] 1.3 Existing golden tests stay green UNCHANGED (`TestCompileHomeNix_CuratedAndRaw`,
      `_BroadenedCurated`, `_BroadenedToggleDisabled`, `_BroadenedRawOverlapErrors`,
      `_MoreCurated`, `_MoreCuratedRawOverlapErrors`, `_RawProgramOverlapErrors`,
      `_StagesFiles`, `_Deterministic`) — confirms the refactor is byte-identical.

## 2. Schema — add the 11 dotfiles/CLI-tier typed fields

- [x] 2.1 RED tests (`home_settings_test.go`): the 11 concepts round-trip through JSONC load
      (`TestLoadManifest_HomeManagerSettings_DotfilesTier`); a mistyped sub-key on each fails
      to load (`...RejectsUnknownDotfilesTierKey`).
- [x] 2.2 `internal/manifest/types.go`: add `Ripgrep`/`Fd`/`Zsh`/`Bash`/`Helix`/`Kitty`/
      `Alacritty`/`Wezterm`/`Jujutsu`/`Atuin`/`Yazi` to `HomeManagerSettings`; add the 11 typed
      structs. DisallowUnknownFields covers them automatically.

## 3. Rendering — one table row per new concept

- [x] 3.1 RED tests (`home_catalog_test.go`): each new concept renders its expected stable
      statement(s) (`TestCompileHomeNix_DotfilesTier`); raw `programs.<name>` colliding with each
      new curated concept is a clear error (`...DotfilesTierRawOverlapErrors`); enable-only emits
      no empty second field (`...DotfilesTierEnableOnly`).
- [x] 3.2 `internal/realizer/nix/home_catalog.go`: add the 11 rows to `curatedTable`.

## 4. Example manifest + OpenSpec + verification

- [x] 4.1 `manifests/examples/home-manager-settings.jsonc`: add `ripgrep`/`fd`/`zsh`/`kitty`
      curated entries + an `lsd` raw-passthrough demo (document the bare-attrset escape hatch).
- [x] 4.2 `cd go-engine && go test ./internal/realizer/... ./internal/manifest/...` green.
- [x] 4.3 `go vet ./internal/realizer/... ./internal/manifest/...` clean; `go build ./...` clean
      (host is windows/amd64, so this is the `GOOS=windows` build).
- [ ] 4.4 **TRANSFERRED, NOT VERIFIED:** `add-linux-product-parity` task 9.15 owns real-nix `home-manager switch`
      activation smoke, ONE per StableField KIND, on a Linux/WSL box with `--enable-restore`:
      - `kindString`   → `zsh` (assert generated `home.nix` has `programs.zsh.initContent` and
        activation succeeds: `.zshrc` body present).
      - `kindStringSlice` → `ripgrep` (assert `programs.ripgrep.arguments` and the ripgreprc /
        `rg` binary on PATH).
      - `kindAnyMap`   → `helix` (assert `programs.helix.settings` and helix `config.toml`).
      - `kindStringMap` → `bat` (already curated; assert `programs.bat.config` still activates).
      - `kindNone`     → `fzf` (already curated; assert `programs.fzf.enable` still activates).
- [x] 4.5 OpenSpec 1.14.0 strict validation passes during the archive batch;
      the validator runs from an isolated tool install without changing dependencies.

## Archive reconciliation — 2026-11-04

Source inspection at `d76d3e5` confirms the descriptor table, derived overlap
guard, eleven typed concepts, generic emission loop, example, and existing
manifest/render regressions. Tasks 1.1–4.3 retain implementation evidence;
Windows build and vet results remain historical, not fresh results from this batch.
Task 4.5 receives current strict validation before and after spec sync.

Task 4.4 stays unchecked. Its complete acceptance transfers to
`add-linux-product-parity` task 9.15: one real `home-manager switch` activation
for each of `kindNone`, `kindString`, `kindStringMap`, `kindAnyMap`, and
`kindStringSlice`, checking the generated statement and activated target.
The five concrete cases above remain acceptance examples, not completed smokes.
The Linux serialized registry/real-Home-Manager packet owns that gate against
the immutable release input pair. This archive performs no Nix activation.

The existing main spec's Purpose and requirements remain intact. This change
adds its three requirements before archive. Later Linux settings and adapter
packets may edit `home_catalog.go` only after this archive batch lands.

The scoped Go recheck passes the 27 selected settings cases in
`internal/manifest` and 34 selected render/flake cases in `internal/realizer/nix`.
These counts include subtests and prove no real Home Manager activation.
