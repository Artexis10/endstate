> TDD: write each test RED first, then implement to green. Hermetic (pure generator; inject the realizer
> via `newRealizerFn`/`fakeRealizer`; override BOTH seams on command tests — the Phase-4 CI gotcha) +
> host-aware. Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`.
> A **real-nix smoke** (throwaway `$HOME`) proves a *generated* flake activates like a hand-written one and
> decides the `flake.lock` question.

## 1. Manifest input

- [ ] 1.1 `internal/manifest/types.go`: add `Config string` (`json:"config,omitempty"`) to `HomeManagerConfig`
- [ ] 1.2 RED test: a manifest with `homeManager.config` parses; the path is retained; absent ⇒ "" 
- [ ] 1.3 RED test + impl: `config` and `flake` are mutually exclusive — a manifest setting both fails to load
      with a clear validation error (add to `ValidateManifestApps`/manifest validation)

## 2. Flake generator (realizer/nix)

- [ ] 2.1 RED tests (`internal/realizer/nix/home_flake_test.go`): the generator renders a `flake.nix` that
      pins `nixpkgs` (`ENDSTATE_NIXPKGS_PIN`) + `home-manager` (`ENDSTATE_HOME_MANAGER_PIN`, nixpkgs follows),
      references the user's `home.nix` by absolute path as a module, injects `home.username` /
      `home.homeDirectory` / `home.stateVersion`, and returns the `<dir>#<name>` flakeref
- [ ] 2.2 Implement the generator: render + write to a stable engine-state location
      (`state/home-manager/<name>/flake.nix`, via `state.StateDir()` — never hardcoded); identity from
      `user.Current()` + home dir (injectable for tests); pins from `defaultPin`/`defaultHomePin`. Plain +
      commented output (inspectable)

## 3. Config stage wiring (realizer path)

- [ ] 3.1 `internal/commands/apply_realizer.go`: when `flags.EnableRestore && mf.HomeManager.Config != ""`,
      resolve the path (relative to the manifest), generate the flake, then call the EXISTING
      `ActivateHome(generatedRef)`; record the generation exactly as the flake path does today
- [ ] 3.2 `--dry-run`: generate + emit the generated flake path + a "would activate" event; activate nothing
- [ ] 3.3 RED command-tests (inject `fakeRealizer`, override BOTH seams): config branch generates and calls
      `ActivateHome` with the generated ref; the generated flake path is recorded/surfaced; `--dry-run`
      generates + reveals but does NOT activate; `flake` path still works unchanged; winget path never generates

## 4. Inspectability

- [ ] 4.1 The generated flake is written plain + commented to the discoverable state path and PERSISTS after
      apply (ejectable). Test: the file exists and is readable after generation
- [ ] 4.2 The Provisioning Generation / events record the activated config (so a generated config is audited
      like a referenced flake)

## 5. Contract (PROTECTED — additive)

- [ ] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.config`, the `config`/`flake` mutual
      exclusion, the engine-generated-and-inspectable flake (stable location, ejectable), and the `--dry-run`
      reveal

## 6. Verification

- [ ] 6.1 `cd go-engine && go test ./...` green on Linux
- [ ] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (realizer path is non-Windows; winget/default untouched)
- [ ] 6.3 `npm run openspec:validate` (strict) passes
- [ ] 6.4 **Real-nix smoke** (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a tiny `home.nix`
      (`programs.git.userName`) referenced by `homeManager.config` → `apply --enable-restore` generates the
      flake → activates → the managed `~/.config/git/config` reflects it; `--dry-run` reveals the generated
      path and activates nothing; the generated flake persists and is ejectable (`nix run home-manager --
      switch --flake <that dir>` works by hand). **Decide the `flake.lock` question** (generate one vs rely on
      pins) and record the verdict in `design.md`/`tasks.md`.
