> TDD: write each test RED first, then implement to green. Hermetic (inject the realizer runner;
> override `newRealizerFn`) + host-aware. Verify: `cd go-engine && go test ./...` (Linux) +
> `GOOS=windows go build ./...` + `go vet`. A **real-nix home-manager smoke** (throwaway `$HOME`)
> on this box validates actual `switch` behavior and decides whether rollback joins this change.

## 1. Manifest field

- [x] 1.1 `internal/manifest/types.go`: add `HomeManager *HomeManagerConfig` (`json:"homeManager,omitempty"`)
      with `Flake string` (`json:"flake"`); ensure JSONC load round-trips it
- [x] 1.2 RED test: a manifest with `homeManager.flake` parses; absent ⇒ nil

## 2. Engine-owned activation (realizer/nix)

- [x] 2.1 RED tests (`internal/realizer/nix/home_manager_test.go`, injected `Runner`): activation runs
      `nix run <pin> -- switch --flake <ref> -b endstate-backup`; success returns the new hm
      generation; non-zero/spawn/permission → classified `*realizer.Error` (raw text in `Raw` only)
- [x] 2.2 `internal/realizer/nix/home_manager.go`: implement the activation via the existing runner;
      pin via `ENDSTATE_HOME_MANAGER_PIN` (default pinned `github:nix-community/home-manager/<rev>`);
      classify through the existing anchor path

## 3. Provisioning Generation record

- [x] 3.1 `internal/provision/provision.go`: add optional `HomeManager *HomeGenRef{ Flake string;
      Generation int }` to `Generation`
- [x] 3.2 Adjust the write so a config-only activation still records a generation (home-manager
      activation counts as "something happened")

## 4. Config stage in apply (realizer path)

- [x] 4.1 `internal/commands/apply_realizer.go`: after the package phases, when `flags.EnableRestore
      && mf.HomeManager != nil && mf.HomeManager.Flake != ""`, activate; emit config-stage events;
      populate the generation's `HomeManager`; systemic failure → top-level envelope error, else
      `INSTALL_FAILED` with raw text in detail
- [x] 4.2 RED tests (inject a fake realizer/runner; override BOTH seams): stage fires only with the
      flag + manifest field, passes the right argv, records the generation field; no flag / no field
      ⇒ no activation; winget path never activates

## 5. Contract (PROTECTED — maintainer-approved, additive)

- [x] 5.1 `docs/contracts/cli-json-contract.md`: document `homeManager.flake` + the `apply
      --enable-restore` config stage (realizer-only, engine-owned, backup-on-clobber, recorded)

## 6. Verification

- [x] 6.1 `cd go-engine && go test ./...` green on Linux
- [x] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean (winget/default path untouched)
- [x] 6.3 `npm run openspec:validate` (strict) passes
- [x] 6.4 **Real-nix home-manager smoke** (throwaway `$HOME` + isolated `ENDSTATE_ROOT`): a tiny
      `home.nix` (e.g. `programs.git.userName`) flake → `apply --enable-restore` activates it →
      generated `~/.gitconfig` reflects it, clobbered file backed up, generation records the config.
      Probe rollback mechanics → decide if rollback joins this change or becomes the fast-follow.

### Smoke outcome (real Determinate Nix 3.21.0, throwaway `$HOME`)

- `nix run github:nix-community/home-manager -- switch --flake <ref> -b endstate-backup` activates cleanly
  (exit 0). Engine `apply --enable-restore` end-to-end: managed `~/.config/git/config` reflects the flake;
  the clobbered file is moved to `config.endstate-backup`; the generation records
  `homeManager: { flake, generation }` (hm gen read from the `$XDG_STATE_HOME/nix/profiles/home-manager`
  `-<N>-link` symlink). Without `--enable-restore` the config is untouched (default apply byte-identical).
- **Generation-number source:** the home-manager nix-profile symlink (`-<N>-link`), read the same way as the
  package generation — no second nix invocation. **Switch output is plain text** (not internal-json), so
  classification anchors the raw stderr; the runner's experimental-features flag must precede the `nix run`
  `--` (the `nixArgs` fix).
- **Rollback verdict: DEFER to fast-follow (as designed).** The mechanism is confirmed feasible —
  re-activate a prior generation's `<store-path>/activate` (store path + id from `home-manager generations`
  / the `-<N>-link` symlink), exit 0 — BUT it produces a NEW forward generation (mirroring the old content)
  rather than moving the pointer back, unlike `nix profile rollback --to`. That is a distinct mechanism from
  the package `Rollbacker` and warrants its own focused change + tests. The probe de-risks the follow-on; the
  orchestration core ships without hm rollback.
