> TDD: write each test RED first, then implement to green. Hermetic (pure compiler + encoder; inject the
> realizer via `newRealizerFn`/`fakeRealizer`; override BOTH seams on command tests — the Phase-4 CI gotcha)
> + host-aware. Reuse #87's `GenerateHomeFlake`/`ActivateHome` and #87's `resolveHomeFlake` seam — do NOT add a
> new activation/recording path. Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...`
> + `go vet`. A **real-nix smoke** (throwaway `$HOME`) proves a declared `settings` block activates and decides
> the open mapping/`xdg` questions.

## 1. Manifest input

- [x] 1.1 `internal/manifest/types.go`: add `Settings *HomeManagerSettings` (`json:"settings,omitempty"`) to
      `HomeManagerConfig`; define `HomeManagerSettings` = curated concepts (`Git`, `Shell`, `Direnv`,
      `Starship`) + `Programs map[string]any` (`json:"programs,omitempty"`) + `Files map[string]string`
      (`json:"files,omitempty"`). Strict `UnmarshalJSON` (DisallowUnknownFields) rejects unknown curated keys.
- [x] 1.2 RED test: a manifest with `homeManager.settings` parses; curated keys, raw `programs`, and `files`
      are retained; absent ⇒ nil
- [x] 1.3 RED test + impl: `settings` / `config` / `flake` are mutually exclusive — a manifest setting more
      than one fails to load with a clear validation error (extend the wrapper's check to three-way)

## 2. Catalog compiler (realizer/nix)

- [x] 2.1 RED tests: a **JSON→Nix value encoder** renders bools / numbers / Nix-escaped strings (incl. `${`
      antiquotation) / lists / attrsets correctly (table-driven; the unit the raw `programs` passthrough needs)
- [x] 2.2 RED tests: the **curated mapping table** maps each v1 concept (`git` via the stable extraConfig,
      `shell` via home.*, `direnv`/`starship` toggles); raw-program/curated overlap is rejected. (Unknown
      curated sub-keys are rejected at parse time — Task 1.)
- [x] 2.3 RED tests: the compiler renders a `home.nix` that contains the mapped curated options, the raw
      `programs` block verbatim, and `home.file` entries for `files`; deterministic output
- [x] 2.4 Implement the compiler (`internal/realizer/nix/home_catalog.go`): pure `settings` → `home.nix` text;
      `files` staging (copy each source beside the flake — binary-safe, pure-eval-safe like #87's `home.nix`
      copy-in); reuse the wrapper's flake writing (extracted `writeHomeFlake`) via
      `GenerateHomeFlakeFromSettings` to return the `<dir>#<name>` flakeref

## 3. Config stage wiring (realizer path)

- [x] 3.1 `internal/commands/apply_realizer.go`: `resolveHomeFlake` gains a `mf.HomeManager.Settings != nil`
      branch → compile to `home.nix` (relative `files` sources resolved against the manifest dir) → generate
      the flake → call the EXISTING `ActivateHome(ref)`; record exactly as today (the config-stage block is
      unchanged — it already records the generated flakeref; `HomeGenRef.Config` is empty for settings since
      capture-into-catalog is deferred)
- [x] 3.2 `--dry-run`: compile + emit the generated `home.nix`/flake path + a "would activate" event; activate
      nothing (reuses the wrapper's generic reveal: `ApplyResult.homeManager{flake,generated,activated}`)
- [x] 3.3 RED command-tests (inject `fakeRealizer`): the `settings` branch compiles and calls `ActivateHome`
      with the generated ref; the compiled `home.nix` + staged files are written/surfaced; `--dry-run` compiles
      + reveals but does NOT activate; `config`/`flake` paths still work unchanged (full commands pkg green)

## 4. Inspectability

- [x] 4.1 The compiled `home.nix`, the staged `files`, and the generated flake are written plain to the
      discoverable state path and PERSIST after apply (ejectable). Test asserts all three exist + the home.nix
      contains the mapped options after generation
- [x] 4.2 The Provisioning Generation records the activated config (the generated flakeref), so a catalog-
      declared config is audited exactly like a referenced flake

## 5. Contract (PROTECTED — additive)

- [x] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.settings`, the curated/raw hybrid, the
      `files` concept (arbitrary text/binary, restore-only), the three-way `settings`/`config`/`flake` mutual
      exclusion, and the `--dry-run` reveal (additive "Declarative catalog" subsection)

## 6. Verification

- [x] 6.1 `cd go-engine && go test ./...` green on Linux
- [x] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (realizer path is non-Windows; winget/default untouched)
- [x] 6.3 `npm run openspec:validate` (strict) passes — 63 passed, 0 failed
- [x] 6.4 **Real-nix smoke** PASS (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a `homeManager.settings` block
      (curated `git.userName`+`defaultBranch`, curated `shell.aliases`, raw `programs.bash`, a `files` dotfile)
      → `apply --enable-restore` compiled the `home.nix` → generated the flake → activated; the managed
      `~/.config/git/config` (`name=smoke-catalog`, `defaultBranch=main`), `~/.bashrc` (`alias ll='ls -la'`),
      and the placed `~/.config/endstate-smoke.txt` all reflected the declaration; `--dry-run` revealed
      (`generated:true, activated:false`) and activated nothing; the generated flake persisted and was ejectable
      (hand-run `nix run home-manager -- switch --flake <dir>#hugoa` → exit 0). Verdicts recorded in `design.md`:
      git→stable `extraConfig` (no deprecation), `home.file` uniformly (no `xdg.configFile`), no engine-generated
      `flake.lock` (Nix self-locks, as #87).
