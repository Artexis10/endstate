> TDD: each Go piece is written RED first, then implemented to green. All CI tests are hermetic (no real
> `brew`); brew's real uninstall anchor is confirmed ONLY by the macOS smoke (a CI leg, not a local gate
> on this Linux box).

## 1. Brew rollback set helper

- [x] 1.1 RED: `brewRollbackRefs(targetGen)` returns the de-duplicated union of `AddedRefs` of every
      `Backend == "brew"` generation numbered greater than `targetGen`; ignores nix/winget generations
      and generations at or below the target.
- [x] 1.2 Implement `brewRollbackRefs` in `rollback.go` (reuses `provision.List()`).

## 2. Two-lane rollback composition

- [x] 2.1 RED: `rollback --to N --confirm` on a capable realizer with a later `backend: "brew"`
      generation performs the native `Rollback` AND uninstalls the brew refs; result carries
      `RemovedRefs`; a separate `backend: "brew"` rollback-marked generation is appended.
- [x] 2.2 RED: a brew uninstall failure → `Partial` with the failed ref reported; the native rollback
      still stands; no top-level error while another succeeded.
- [x] 2.3 RED: a brew-only target generation (no native anchor) with later brew refs is valid — the brew
      lane runs, the native `Rollback` is NOT called.
- [x] 2.4 RED (non-regression): a no-brew history with `--to N` never resolves `newBrewDriverFn`
      (`panicBrewDriverFn`) and yields the native-only result; bare rollback (no `--to`) leaves brew
      untouched (brew driver never resolved).
- [x] 2.5 Implement: `runBrewRollbackLane` (best-effort uninstall + `appendRollbackGenerationRemoved`
      with backend `brew`); compute `brewRemoveRefs` in `runRealizerRollback` only when `--to` is given;
      relax the nothing-to-rollback guard with `len(brewRemoveRefs) == 0`; guard the native generation
      append to `hasPackageTarget || newHomeRef != nil`; fold brew removed/failed/partial/warning into
      `RollbackResult`.

## 3. Confirm gate + dry-run

- [x] 3.1 RED: `rollback --to N --dry-run` previews the brew `RemovedRefs` without uninstalling or
      appending a generation; `rollback --to N` without `--confirm` refuses, uninstalls nothing, appends
      nothing (the existing native gate already covers this — assert the brew lane respects it).
- [x] 3.2 Implement the dry-run brew preview branch.

## 4. Test double

- [x] 4.1 Extend `fakeBrewDriver` (in `apply_brew_test.go`) with `Uninstall` (records refs, scriptable
      per-ref outcome + infra error) so it satisfies `driver.Uninstaller`.

## 5. macOS smoke + CI

- [x] 5.1 Extend `scripts/smoke/brew-realbrew-smoke.sh`: after apply+capture, apply a second tiny formula
      (best-effort), then `rollback --to <gen> --confirm`, then assert the later formula is uninstalled
      (`brew list` fails) while the first remains, and the rollback output reports the removed ref.
- [x] 5.2 No workflow change needed — the script path is already in the `nix-integration.yml` path
      filter, so editing it triggers the macOS smoke leg (the `command -v brew` guard no-ops on linux).

## 6. OpenSpec + verification

- [x] 6.1 This change (`macos-brew-best-effort-rollback`) with distinct requirement names; `openspec
      validate --all --strict` green.
- [x] 6.2 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` green.
- [x] 6.3 Existing nix home-manager smoke confirms the realizer rollback lane is unbroken (non-regression
      on the high-blast-radius change).
- [x] 6.4 macOS brew rollback smoke confirmed green (CI-only; cannot run on the Linux dev box).
