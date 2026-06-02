> TDD: write each test RED first, then implement to green. Hermetic (pure generator; inject the realizer
> via `newRealizerFn`/`fakeRealizer`; override BOTH seams on command tests — the Phase-4 CI gotcha) +
> host-aware. Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`.
> A **real-nix smoke** (throwaway `$HOME`) proves a *generated* flake activates like a hand-written one and
> decides the `flake.lock` question.

## 1. Manifest input

- [x] 1.1 `internal/manifest/types.go`: add `Config string` (`json:"config,omitempty"`) to `HomeManagerConfig`
- [x] 1.2 RED test: a manifest with `homeManager.config` parses; the path is retained; absent ⇒ "" 
- [x] 1.3 RED test + impl: `config` and `flake` are mutually exclusive — a manifest setting both fails to load
      with a clear validation error (add to `ValidateManifestApps`/manifest validation)

## 2. Flake generator (realizer/nix)

- [x] 2.1 RED tests (`internal/realizer/nix/home_flake_test.go`): the generator renders a `flake.nix` that
      pins `nixpkgs` (`ENDSTATE_NIXPKGS_PIN`) + `home-manager` (`ENDSTATE_HOME_MANAGER_PIN`, nixpkgs follows),
      references the user's `home.nix` as a module, injects `home.username` /
      `home.homeDirectory` / `home.stateVersion`, and returns the `<dir>#<name>` flakeref
      **[SMOKE DEVIATION — see §6.4: pure-eval forbids an absolute-path module ref, so the user's `home.nix`
      is COPIED into the generated dir and referenced as `./home.nix` (also makes the flake self-contained +
      ejectable). The test asserts `./home.nix`, not an absolute path.]**
- [x] 2.2 Implement the generator: render + write to a stable engine-state location
      (`state/home-manager/<name>/{flake.nix,home.nix}`, via `state.StateDir()` — never hardcoded); identity
      from `user.Current()` + home dir (injectable for tests); pins from `defaultPin`/`defaultHomePin`; host
      system from the Go runtime. Plain + commented output (inspectable). `<name>` = current username.

## 3. Config stage wiring (realizer path)

- [x] 3.1 `internal/commands/apply_realizer.go`: when `flags.EnableRestore && mf.HomeManager.Config != ""`,
      resolve the path (relative to the manifest), generate the flake, then call the EXISTING
      `ActivateHome(generatedRef)`; record the generation exactly as the flake path does today
      (via `resolveHomeFlake`, shared by the dry-run + real paths)
- [x] 3.2 `--dry-run`: generate + emit the generated flake path + a "would activate" event; activate nothing
- [x] 3.3 RED command-tests (inject `fakeRealizer`, override BOTH seams): config branch generates and calls
      `ActivateHome` with the generated ref; the generated flake path is recorded/surfaced; `--dry-run`
      generates + reveals but does NOT activate; `flake` path still works unchanged; winget path never generates

## 4. Inspectability

- [x] 4.1 The generated flake is written plain + commented to the discoverable state path and PERSISTS after
      apply (ejectable). Test: `flake.nix` + the copied `home.nix` exist and are readable after generation
- [x] 4.2 The Provisioning Generation / events record the activated config (so a generated config is audited
      like a referenced flake) — recorded under the generated `<dir>#<name>` flakeref

## 5. Contract (PROTECTED — additive)

- [x] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.config`, the `config`/`flake` mutual
      exclusion, the engine-generated-and-inspectable flake (stable location, ejectable), and the `--dry-run`
      reveal (additive subsection + the apply-result `homeManager` object)

## 6. Verification

- [x] 6.1 `cd go-engine && go test ./...` green on Linux
- [x] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (realizer path is non-Windows; winget/default untouched)
- [x] 6.3 `npm run openspec:validate` (strict) passes — 60 passed, 0 failed
- [x] 6.4 **Real-nix smoke** (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a tiny `home.nix`
      (`programs.git.userName`) referenced by `homeManager.config` → `apply --enable-restore` generated the
      flake → activated → the managed `~/.config/git/config` reflected it (`name = "smoke-engine"`); the
      generation recorded `homeManager{flake:<gen dir>#hugoa, generation:1}`; `--dry-run` revealed the
      generated path (`generated:true, activated:false`) and activated nothing; the generated flake persisted
      and was ejectable (`nix run home-manager -- switch --flake <that dir>#hugoa` by hand → exit 0).
      **`flake.lock` VERDICT (recorded in design.md): do NOT engine-generate a lock — rely on the engine pins
      (matches #81); Nix auto-writes its own lock into the persistent state dir.** Also recorded the forced
      DEVIATION: pure-eval forbids an absolute-path module ref, so the user's `home.nix` is copied in and
      referenced as `./home.nix` (which also makes the generated dir a self-contained, ejectable flake).
