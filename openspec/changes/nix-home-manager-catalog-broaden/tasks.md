> TDD: write each test RED first, then implement to green. Hermetic (no live Nix calls). Verify:
> `cd go-engine && go test ./...` + `go vet ./...` + `GOOS=windows go build ./...`; real-nix apply smoke.

## 1. Schema — add typed curated fields

- [x] 1.1 RED tests (`home_settings_test.go`): the new concepts (`fzf`, `zoxide`, `bat`, `tmux`, `ssh`)
      round-trip through JSONC load; a mistyped sub-key (`bat.confgi`, `tmux.extraConfigg`,
      `ssh.extarConfig`, `fzf.enabel`) fails to load.
- [x] 1.2 `internal/manifest/types.go`: add `Fzf`/`Zoxide` (`*ProgramToggle`), `Bat` (`*BatSettings`),
      `Tmux` (`*TmuxSettings`), `SSH` (`*SSHSettings`) to `HomeManagerSettings`; add the three small
      typed structs.

## 2. Rendering — map each concept to a stable home-manager option

- [x] 2.1 RED tests (`home_catalog_test.go`): each new concept renders its expected statement(s)
      (`programs.fzf.enable`, `programs.bat.config`, `programs.tmux.extraConfig`,
      `programs.ssh.extraConfig`, …); an explicit `enable=false` toggle renders; a raw `programs.<name>`
      colliding with a curated concept (all five) is a clear error; determinism preserved.
- [x] 2.2 `internal/realizer/nix/home_catalog.go`: add the five names to `curatedPrograms`; render each
      concept in `CompileHomeNix` following the git/direnv/starship patterns (sorted `bat.config`).
- [x] 2.3 Fix the two pre-existing tests that used `bat`/`fzf` as raw-passthrough placeholders to use a
      non-curated name (`htop`/`lsd`).

## 3. Example manifest

- [x] 3.1 `manifests/examples/home-manager-settings.jsonc`: curated concepts (incl. 2-3 new) + a raw
      `programs` block + a `files` entry; stage the `files` source under `hm-settings-assets/` (NOT
      `payload/`, which is gitignored — so the example is self-consistent in the repo).
- [x] 3.2 Confirm it loads (`endstate plan --manifest …`).

## 4. OpenSpec + verification

- [x] 4.1 `cd go-engine && go test ./...` green
- [x] 4.2 `go vet ./...` + `GOOS=windows go build ./...` clean
- [x] 4.3 `openspec validate nix-home-manager-catalog-broaden --strict --no-interactive` passes
- [ ] 4.4 Real-nix apply smoke (operator-run): apply a manifest with 2-3 new concepts
      (`--enable-restore`) and assert the activated home-manager config exposes them.
