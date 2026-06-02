> TDD: write each test RED first, then implement to green. Hermetic (inject the realizer runner;
> override `newRealizerFn`) + host-aware. Verify: `cd go-engine && go test ./...` (Linux) +
> `GOOS=windows go build ./...` + `go vet`. A **real-nix home-manager smoke** (throwaway `$HOME`)
> on this box validates actual `switch` behavior and decides whether rollback joins this change.

## 1. Manifest field

- [ ] 1.1 `internal/manifest/types.go`: add `HomeManager *HomeManagerConfig` (`json:"homeManager,omitempty"`)
      with `Flake string` (`json:"flake"`); ensure JSONC load round-trips it
- [ ] 1.2 RED test: a manifest with `homeManager.flake` parses; absent ⇒ nil

## 2. Engine-owned activation (realizer/nix)

- [ ] 2.1 RED tests (`internal/realizer/nix/home_manager_test.go`, injected `Runner`): activation runs
      `nix run <pin> -- switch --flake <ref> -b endstate-backup`; success returns the new hm
      generation; non-zero/spawn/permission → classified `*realizer.Error` (raw text in `Raw` only)
- [ ] 2.2 `internal/realizer/nix/home_manager.go`: implement the activation via the existing runner;
      pin via `ENDSTATE_HOME_MANAGER_PIN` (default pinned `github:nix-community/home-manager/<rev>`);
      classify through the existing anchor path

## 3. Provisioning Generation record

- [ ] 3.1 `internal/provision/provision.go`: add optional `HomeManager *HomeGenRef{ Flake string;
      Generation int }` to `Generation`
- [ ] 3.2 Adjust the write so a config-only activation still records a generation (home-manager
      activation counts as "something happened")

## 4. Config stage in apply (realizer path)

- [ ] 4.1 `internal/commands/apply_realizer.go`: after the package phases, when `flags.EnableRestore
      && mf.HomeManager != nil && mf.HomeManager.Flake != ""`, activate; emit config-stage events;
      populate the generation's `HomeManager`; systemic failure → top-level envelope error, else
      `INSTALL_FAILED` with raw text in detail
- [ ] 4.2 RED tests (inject a fake realizer/runner; override BOTH seams): stage fires only with the
      flag + manifest field, passes the right argv, records the generation field; no flag / no field
      ⇒ no activation; winget path never activates

## 5. Contract (PROTECTED — maintainer-approved, additive)

- [ ] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.flake` + the `apply
      --enable-restore` config stage (realizer-only, engine-owned, backup-on-clobber, recorded)

## 6. Verification

- [ ] 6.1 `cd go-engine && go test ./...` green on Linux
- [ ] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (winget/default path untouched)
- [ ] 6.3 `npm run openspec:validate` (strict) passes
- [ ] 6.4 **Real-nix home-manager smoke** (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a tiny
      `home.nix` (e.g. `programs.git.userName`) flake → `apply --enable-restore` activates it →
      generated `~/.gitconfig` reflects it, clobbered file backed up, generation records the config.
      Probe rollback mechanics → decide if rollback joins this change or becomes the fast-follow.
