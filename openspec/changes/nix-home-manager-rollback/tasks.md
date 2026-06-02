> TDD: write each test RED first, then implement to green. Hermetic — realizer tests inject a `runScript`
> seam + a temp `XDG_STATE_HOME` with a `home-manager-<M>-link` symlink; command tests inject `fakeRealizer`
> (extended to a `HomeRollbacker`) and override the realizer seam. Verify: `cd go-engine && go test ./...`
> (Linux) + `GOOS=windows go build ./...` + `go vet`. A **real-nix smoke** (sandbox `$HOME`/`ENDSTATE_ROOT`)
> proves apply A → apply B → `rollback --to 1 --enable-restore` reverts the config to A.
>
> Empirical verdict (confirmed before this spec): re-running an old hm generation's `activate` mints a NEW
> FORWARD generation and reverts the config (append-only). See design.md.

## 1. Realizer capability

- [ ] 1.1 `internal/realizer/realizer.go`: add optional `HomeRollbacker { RollbackHome(generation int) (newGen int, err error) }`
      with a doc-comment in the `Pruner` / `HomeActivator` style (type-asserted, optional).

## 2. Nix backend RollbackHome

- [ ] 2.1 `internal/realizer/nix/nix.go`: add an injectable script-exec seam to `Backend` (e.g.
      `runScript func(path string, args ...string) (stdout, stderr []byte, exit int, err error)`), defaulting
      to a real `exec.Command` runner; wire the default in `New()`.
- [ ] 2.2 RED tests (`internal/realizer/nix/home_rollback_test.go`): `RollbackHome(M)` resolves
      `<homeProfilePath>-<M>-link`, reads the store path, execs `<store>/activate`, and returns the new
      generation (via the `homeGenFn` seam); a missing `-<M>-link` returns a *distinguishable* error and does
      NOT exec; non-zero exit with eval/permission/daemon stderr classifies as ROLLBACK_FAILED / PERMISSION_DENIED
      / REALIZER_UNAVAILABLE; a spawn error → REALIZER_UNAVAILABLE; raw text confined to `Err.Raw`.
- [ ] 2.3 Implement `(*Backend).RollbackHome` (new `home_rollback.go` or in `home_manager.go`): reuse
      `homeProfilePath`/`homeGen`/`parsePlainLog`/`classify`, remap the `INSTALL_FAILED` fallback to
      `ROLLBACK_FAILED` (Stage "rollback") so the error names the verb (mirrors package `Rollback`); add
      `var _ realizer.HomeRollbacker = (*Backend)(nil)`.

## 3. Command-layer coupling (rollback.go)

- [ ] 3.1 `internal/commands/rollback.go`: add `EnableRestore bool` to `RollbackFlags`; add a
      `HomeManager *RollbackHomeResult` sub-object to `RollbackResult` (target hm gen, new hm gen, flake/config,
      dry-run-aware).
- [ ] 3.2 `appendRollbackGeneration` (apply_generation.go or local): accept an optional `*provision.HomeGenRef`
      so the appended rollback generation records the now-active config.
- [ ] 3.3 RED command tests + impl in `runRealizerRollback`: validate config eligibility BEFORE mutating
      (type-assert `HomeRollbacker`; load gen N; read `HomeManager`); `--enable-restore` + config present +
      non-`HomeRollbacker` → `ROLLBACK_UNSUPPORTED` (nothing mutated); `--dry-run` previews config target, no
      call, no append; on confirm: package `Rollback` then `RollbackHome(home.Generation)`; fallback on the
      missing-snapshot signal (direct flake → `ActivateHome(home.Flake)`; wrapper → `ROLLBACK_FAILED` +
      remediation); append one combined generation recording `{Flake, Config from gen N, Generation: newHmGen}`.
- [ ] 3.4 Tests: coupled success records `HomeManager.Generation == newHmGen` and rolls packages too; no
      `--enable-restore` ⇒ no `RollbackHome` (backward-compat); gen N has no config ⇒ package-only success;
      config failure systemic vs non-systemic classification with raw confined to detail.
- [ ] 3.5 `TestRollbackStaysPackageOnly` stays green — `rollback.go` MUST NOT import `internal/restore`.

## 4. CLI wiring (PROTECTED — additive)

- [ ] 4.1 `cmd/endstate/main.go`: add `EnableRestore: p.enableRestore` to the `rollback` case; update the
      `rollback` usage/help text to note `--enable-restore` also reverts the recorded home-manager config.

## 5. Verification

- [ ] 5.1 `cd go-engine && go test ./...` green on Linux.
- [ ] 5.2 `GOOS=windows go build ./...` + `go vet ./...` clean (realizer/home-manager path is non-Windows;
      winget/default rollback untouched).
- [ ] 5.3 `npm run openspec:validate` (strict) passes.
- [ ] 5.4 **Real-nix e2e smoke** (sandbox `$HOME` + isolated `ENDSTATE_ROOT`; hm pin
      `git+file:///home/hugoa/projects/home-manager`): apply config A (marker) → apply config B → 
      `rollback --to 1 --enable-restore --confirm` → assert the managed config reverts to A (B's marker gone)
      and a rollback-marked Provisioning Generation recording the home ref was appended. Real `$HOME` untouched.
