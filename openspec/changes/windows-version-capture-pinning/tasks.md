> TDD: write each test RED first, then implement to green. Hermetic + host-aware (inject
> `ExecCommand`/snapshot fixtures and `newDriverFn`/`newRealizerFn`; key apply fixtures by
> `runtime.GOOS`). Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...`
> + `go vet`. Real-winget pin/capture smoke is maintainer-side on Windows (no winget on this box).

## 1. Capture: driver result carries version

- [x] 1.1 `internal/driver/driver.go`: add `Version string` to `DetectResult`
- [x] 1.2 RED test (`internal/driver/winget/detect_test.go`): `DetectBatch` populates
      `DetectResult.Version` from the snapshot's parsed Version column
- [x] 1.3 `internal/driver/winget/detect.go`: `DetectBatch` copies `SnapshotApp.Version` into
      `DetectResult.Version` (no new winget call)

## 2. Capture: apply records the version in the generation

- [x] 2.1 `internal/commands/apply.go`: add `Version string` to `ApplyAction`; set it from
      `batchResults[ref].Version` for present packages
- [x] 2.2 `internal/commands/apply_generation.go`: copy `ApplyAction.Version` into
      `ProvItem.Version`
- [x] 2.3 RED test: driver-path `apply` writes a generation whose installed/present item carries the
      captured version; nix-path generation keeps `version: ""`

## 3. Pinning: VersionedInstaller

- [x] 3.1 `internal/driver/driver.go`: add optional `VersionedInstaller` interface
      (`InstallVersion(ref, version string) (*InstallResult, error)`)
- [x] 3.2 RED tests (`internal/driver/winget/install_version_test.go`, injected `ExecCommand`):
      `InstallVersion` passes `--version <v>`; exit 0 → installed; unavailable (non-zero,
      non-already-installed) → `StatusFailed`/`ReasonInstallFailed`
- [x] 3.3 `internal/driver/winget/install_version.go`: implement `InstallVersion` via `winget
      install --version`; reuse the existing exit-code classification; compile-time assert
      `*WingetDriver` satisfies `driver.VersionedInstaller`

## 4. Pinning: apply honors App.Version

- [x] 4.1 `internal/commands/apply.go` (driver path): when `app.Version != ""` and the driver is a
      `VersionedInstaller`, install via `InstallVersion`; else `Install`. On a pinned success set
      `action.Version = app.Version`
- [x] 4.2 RED tests (call the driver-path apply with a mock that records args; override BOTH
      `newDriverFn` AND `newRealizerFn`): pinned manifest → `InstallVersion(ref, "x")` called,
      generation records version "x"; no-version manifest → `Install` called, no `--version`;
      unavailable pin → item `failed`/`install_failed`
- [x] 4.3 RED test: nix realizer path ignores `App.Version` (no error; installs via ref)

## 5. Contract (PROTECTED — maintainer-approved)

- [x] 5.1 `docs/ai/AI_CONTRACT.md`: reword Non-Goal #3 to allow opt-in, declared version pinning
      (preserve the no-automatic-version-management spirit)
- [x] 5.2 `docs/contracts/cli-json-contract.md`: document `App.Version` pinning + the populated
      `version` field on winget generations

## 6. Verification

- [x] 6.1 `cd go-engine && go test ./...` green on Linux
- [x] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [x] 6.3 `npm run openspec:validate` (strict) passes
- [ ] 6.4 (Maintainer, Windows) real-winget smoke: pinned `App.Version` installs that exact version
      and the generation records it; an unavailable version → `INSTALL_FAILED`; an unpinned app
      installs latest and records its version
