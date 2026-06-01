> TDD: write each test RED first, then implement to green. Hermetic + host-aware (mockDriver
> `versions` map; override BOTH `newDriverFn` AND `newRealizerFn`; key fixtures by `runtime.GOOS`).
> Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`. The
> real-winget `--force` reinstall smoke is maintainer-side on Windows.

## 1. Detect: verify reports version drift

- [x] 1.1 `internal/driver/driver.go`: add `ReasonVersionDrift = "version_drift"`
- [x] 1.2 `internal/commands/verify.go`: add `Version`/`Expected` (omitempty) to `VerifyItem`
- [x] 1.3 RED tests (mockDriver `versions` map): declared≠installed → `fail`/`version_drift` with
      `version`/`expected`; declared==installed → pass; no declared version → pass; installed
      version empty → pass (no false drift)
- [x] 1.4 `internal/commands/verify.go`: drift comparison in the winget pass branch (exact, trimmed);
      leave `runVerifyRealizer` untouched

## 2. Converge: VersionedInstaller.ReinstallVersion

- [x] 2.1 `internal/driver/driver.go`: extend `VersionedInstaller` with `ReinstallVersion(ref,
      version) (*InstallResult, error)`
- [x] 2.2 RED tests (`install_version_test.go`, injected `ExecCommand`): `ReinstallVersion` passes
      `--version <v>` AND `--force`; exit 0 → installed; non-zero → install_failed
- [x] 2.3 `internal/driver/winget/`: add `force bool` to the shared `install` helper;
      `Install`/`InstallVersion` pass `false`, `ReinstallVersion` passes `true`; update the
      compile-time `VersionedInstaller` assertion

## 3. Converge: apply --repin

- [x] 3.1 `internal/commands/apply.go`: add `ApplyFlags.Repin`; `appPlan.repin`; mark drifted pinned
      present apps as converge entries in the plan loop
- [x] 3.2 apply loop: `--repin && !--confirm && !--dry-run` → refuse (`INTERNAL_ERROR`); repin entry
      → `ReinstallVersion(ref, app.Version)`; success records `installed`/version; `--dry-run`
      previews without reinstalling; realizer path ignores `--repin`
- [x] 3.3 RED tests (override BOTH seams): `--repin --confirm` on drift → `ReinstallVersion` called,
      generation records declared version; `--repin --dry-run` → not called, previewed; `--repin`
      without `--confirm` → refuses; no `--repin` → drift untouched
- [x] 3.4 `cmd/endstate/main.go` (PROTECTED, additive): parse `--repin`; usage line

## 4. Contract (PROTECTED — maintainer-approved)

- [x] 4.1 `docs/ai/AI_CONTRACT.md`: reword Non-Goal #3 (opt-in compare in verify + confirmed
      converge in apply; no silent/automatic version changes)
- [x] 4.2 `docs/contracts/cli-json-contract.md`: `verify` `version_drift` reason + `version`/
      `expected` fields; `apply --repin` (winget-only, `--confirm`-gated, `--dry-run` preview)

## 5. Verification

- [x] 5.1 `cd go-engine && go test ./...` green on Linux
- [x] 5.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [x] 5.3 `npm run openspec:validate` (strict) passes
- [ ] 5.4 (Maintainer, Windows) real-winget smoke: a drifted pinned app → `verify` reports
      `version_drift`; `apply --repin --confirm` reinstalls the declared version (`--force` downgrades;
      else fall back to uninstall+install); `--repin` without `--confirm` refuses
