> TDD: write each test RED first, then implement to green. Hermetic (no live Nix calls). Verify:
> `cd go-engine && go test ./...` + `go vet ./...` + `GOOS=windows go build ./...`; real-nix apply smoke.

## 1. Schema — add typed curated fields

- [x] 1.1 RED tests (`home_settings_test.go`): the new concepts (`eza`, `gh`, `lazygit`, `neovim`)
      round-trip through JSONC load; a mistyped sub-key (`eza.enabel`, `gh.settigns`,
      `lazygit.settigns`, `neovim.extraCfg`) fails to load.
- [x] 1.2 `internal/manifest/types.go`: add `Eza` (`*EzaSettings`), `Gh` (`*GhSettings`),
      `Lazygit` (`*LazygitSettings`), `Neovim` (`*NeovimSettings`) to `HomeManagerSettings`;
      add the four small typed structs.

## 2. Rendering — map each concept to a stable home-manager option

- [x] 2.1 RED tests (`home_catalog_test.go`): each new concept renders its expected statement(s)
      (`programs.eza.enable`, `programs.eza.extraOptions`, `programs.gh.settings`,
      `programs.lazygit.settings`, `programs.neovim.extraConfig`, …); raw `programs.<name>`
      colliding with a curated concept (all four) is a clear error.
- [x] 2.2 `internal/realizer/nix/home_catalog.go`: add the four names to `curatedPrograms`; render
      each concept in `CompileHomeNix` following the git/bat/tmux/ssh patterns; add
      `stringSliceToAny` helper next to `stringMapToAny`.

## 3. Example manifest

- [x] 3.1 `manifests/examples/home-manager-settings.jsonc`: add `eza` + `neovim` curated entries;
      leave `programs.htop` raw passthrough intact (htop is NOT curated).

## 4. OpenSpec + verification

- [x] 4.1 `cd go-engine && go test ./...` green
- [x] 4.2 `go vet ./...` + `GOOS=windows go build ./...` clean
- [x] 4.3 `openspec validate nix-home-manager-catalog-more --strict --no-interactive` passes
- [ ] 4.4 Real-nix apply smoke (operator-run): apply a manifest with 2-3 new concepts
      (`--enable-restore`) and assert the generated `home.nix` contains the expected statements
      and home-manager activation succeeds.
