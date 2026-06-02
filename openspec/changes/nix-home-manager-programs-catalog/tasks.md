> TDD: write each test RED first, then implement to green. Hermetic (pure compiler + encoder; inject the
> realizer via `newRealizerFn`/`fakeRealizer`; override BOTH seams on command tests — the Phase-4 CI gotcha)
> + host-aware. Reuse #87's `GenerateHomeFlake`/`ActivateHome` and #87's `resolveHomeFlake` seam — do NOT add a
> new activation/recording path. Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...`
> + `go vet`. A **real-nix smoke** (throwaway `$HOME`) proves a declared `settings` block activates and decides
> the open mapping/`xdg` questions.

## 1. Manifest input

- [ ] 1.1 `internal/manifest/types.go`: add `Settings *HomeManagerSettings` (`json:"settings,omitempty"`) to
      `HomeManagerConfig`; define `HomeManagerSettings` = curated concepts (`Git`, `Shell`, `Direnv`,
      `Starship`) + `Programs map[string]any` (`json:"programs,omitempty"`) + `Files map[string]string`
      (`json:"files,omitempty"`)
- [ ] 1.2 RED test: a manifest with `homeManager.settings` parses; curated keys, raw `programs`, and `files`
      are retained; absent ⇒ nil
- [ ] 1.3 RED test + impl: `settings` / `config` / `flake` are mutually exclusive — a manifest setting more
      than one fails to load with a clear validation error (extend the wrapper's check to three-way)

## 2. Catalog compiler (realizer/nix)

- [ ] 2.1 RED tests: a **JSON→Nix value encoder** renders bools / numbers / Nix-escaped strings / lists /
      attrsets correctly (table-driven; the unit the raw `programs` passthrough depends on)
- [ ] 2.2 RED tests: the **curated mapping table** maps each v1 concept (`git`, `shell`, `direnv`, `starship`)
      to its home-manager option(s); unknown curated sub-keys are rejected
- [ ] 2.3 RED tests: the compiler renders a `home.nix` that contains the mapped curated options, the raw
      `programs` block verbatim, and `home.file`/`xdg.configFile` entries for `files`; deterministic output
- [ ] 2.4 Implement the compiler (`internal/realizer/nix/home_catalog.go`): pure `settings` → `home.nix` text;
      `files` staging (copy each source beside the flake — binary-safe, pure-eval-safe like #87's `home.nix`
      copy-in); then reuse `GenerateHomeFlake` to wrap + return the `<dir>#<name>` flakeref

## 3. Config stage wiring (realizer path)

- [ ] 3.1 `internal/commands/apply_realizer.go`: `resolveHomeFlake` gains a `mf.HomeManager.Settings != nil`
      branch → compile to `home.nix` (relative `files` sources resolved against the manifest dir) → generate
      the flake → call the EXISTING `ActivateHome(ref)`; record exactly as today
- [ ] 3.2 `--dry-run`: compile + emit the generated `home.nix`/flake path + a "would activate" event; activate
      nothing (reuse the wrapper's reveal: `ApplyResult.homeManager{flake,generated,activated}`)
- [ ] 3.3 RED command-tests (inject `fakeRealizer`, override BOTH seams): the `settings` branch compiles and
      calls `ActivateHome` with the generated ref; the generated `home.nix` + staged files are written/surfaced;
      `--dry-run` compiles + reveals but does NOT activate; `config`/`flake` paths still work unchanged; winget
      path never compiles

## 4. Inspectability

- [ ] 4.1 The generated `home.nix`, the staged `files`, and the generated flake are written plain to the
      discoverable state path and PERSIST after apply (ejectable). Test: the files exist and are readable
- [ ] 4.2 The Provisioning Generation records the activated config (the generated flakeref), so a catalog-
      declared config is audited exactly like a referenced flake

## 5. Contract (PROTECTED — additive)

- [ ] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.settings`, the curated/raw hybrid, the
      `files` concept (arbitrary text/binary, restore-only), the three-way `settings`/`config`/`flake` mutual
      exclusion, and the `--dry-run` reveal

## 6. Verification

- [ ] 6.1 `cd go-engine && go test ./...` green on Linux
- [ ] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (realizer path is non-Windows; winget/default untouched)
- [ ] 6.3 `npm run openspec:validate` (strict) passes
- [ ] 6.4 **Real-nix smoke** (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a `homeManager.settings` block
      (a curated `git.userName`, one raw `programs` entry, one `files` dotfile) → `apply --enable-restore`
      compiles the `home.nix` → generates the flake → activates → the managed git config AND the placed file
      reflect the declaration; `--dry-run` reveals the generated `home.nix`/flake and activates nothing; both
      persist and are ejectable. **Record the confirmed curated→home-manager mappings** and the
      `xdg.configFile`-vs-`home.file` decision in `design.md`, the way #81/#87 recorded their smoke verdicts.
