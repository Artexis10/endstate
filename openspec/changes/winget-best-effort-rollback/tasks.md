> TDD: write each test RED first, then implement to green. All CI tests are hermetic (no real
> `winget`) — use the `WingetDriver.ExecCommand` injection seam and inject `newDriverFn`/
> `newRealizerFn` in command tests (host-aware, per the Phase-3 pattern). This WSL box has no
> winget: real-winget uninstall smoke is the maintainer's Windows side. Verify here:
> `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`.

## 0. Gate

- [ ] 0.1 `npm run openspec:validate` (strict) passes for this change
- [ ] 0.2 **PAUSE for maintainer review of this spec** before any Go is written

## 1. `provision.Generation` — RemovedRefs (additive)

- [ ] 1.1 Add optional `RemovedRefs []string \`json:"removedRefs,omitempty"\`` to `Generation`
- [ ] 1.2 RED test: round-trips; omitted when empty; `SchemaVersion` stays `"1.0"`

## 2. `driver.Uninstaller` + winget implementation

- [ ] 2.1 `internal/driver/driver.go`: `UninstallResult{Status,Message}` + status constants
      (`uninstalled`/`absent`/`failed`) + `Uninstaller` optional interface
- [ ] 2.2 RED tests (`internal/driver/winget/uninstall_test.go`, `ExecCommand` shim): exit 0 →
      `uninstalled`; "no installed package found" → `absent`; other non-zero → `failed`; missing
      binary → `ErrWingetNotAvailable`. Assert the spawned argv (`winget uninstall --id <ref> -e
      --silent --accept-source-agreements`)
- [ ] 2.3 `internal/driver/winget/uninstall.go`: implement `Uninstall(ref)`; classify via exit
      code + output-substring fallback. **Flag the not-found exit code for maintainer Windows
      verification** (mirrors install already-installed code)
- [ ] 2.4 Compile-time assertion `*winget.WingetDriver` satisfies `driver.Uninstaller`

## 3. `rollback` command — driver (best-effort) path

- [ ] 3.1 Refactor `RunRollback` to dispatch: realizer present → existing native path
      (`runRealizerRollback`); else driver present + `Uninstaller` → `runDriverRollback`; else
      `ROLLBACK_UNSUPPORTED`
- [ ] 3.2 `runDriverRollback`: resolve target N (`--to`; bare = most recent); compute
      `removeRefs` = union of `addedRefs` of generations with `Number > N`; empty → success no-op
- [ ] 3.3 `--dry-run` → preview `removeRefs`, no uninstall, no generation. No `--confirm`
      (and not dry-run) → refuse (message names `--confirm`), no mutation
- [ ] 3.4 Per-ref `Uninstall`: collect `{removed, absent→removed, failed}`; never abort early;
      emit the untracked-dependency warning
- [ ] 3.5 On ≥1 removed: append generation (`Backend="winget"`, `Rollback=true`, `AddedRefs=[]`,
      `RemovedRefs`, `Partial`=any failure)
- [ ] 3.6 Result envelope: success + summary {removed, absent, failed, partial}; top-level error
      only for `WINGET_NOT_AVAILABLE` (binary missing) or `ROLLBACK_FAILED` (every uninstall failed)

## 4. Command tests (`internal/commands/rollback_test.go`, host-aware)

- [ ] 4.1 Add a `fakeUninstaller` driver stub (scriptable per-ref outcomes + arg capture);
      inject via `newDriverFn` (and `newRealizerFn` → `ErrNoRealizer` to force the driver path)
- [ ] 4.2 `--to N` → uninstalls union of `addedRefs` of generations > N; appends a
      rollback-marked generation with `RemovedRefs` + empty `AddedRefs`
- [ ] 4.3 Bare rollback → targets the most recent generation
- [ ] 4.4 Driver not an `Uninstaller` (and no realizer) → `ROLLBACK_UNSUPPORTED`
- [ ] 4.5 Unknown `--to` → `GENERATION_NOT_FOUND`; no uninstall
- [ ] 4.6 Per-ref failure → run continues, result partial, generation `Partial=true`
- [ ] 4.7 Already-absent ref → counted removed, no error
- [ ] 4.8 Every uninstall fails → `ROLLBACK_FAILED`
- [ ] 4.9 No `--confirm` → refuses, nothing uninstalled, no generation
- [ ] 4.10 `--dry-run` → previews `removeRefs`, no uninstall, no generation
- [ ] 4.11 Nothing to roll back (target is most recent) → success no-op, no generation
- [ ] 4.12 Extend the rollback import-guard test to the driver path (no `internal/restore`)

## 5. Windows no-regression

- [ ] 5.1 Existing winget tests stay green; new uninstall tests are hermetic
- [ ] 5.2 `GOOS=windows go build ./...` + `go vet ./...` clean

## 6. Contract documentation (PROTECTED — maintainer-approved, additive)

- [ ] 6.1 `docs/contracts/cli-json-contract.md`: extend `## Command: rollback` with the winget
      best-effort data shape (removed/absent/failed counts, `partial`, `removedRefs`,
      orphan-warning note). No new error codes; no `main.go` change

## 7. Verification

- [ ] 7.1 `cd go-engine && go test ./...` green on Linux
- [ ] 7.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [ ] 7.3 `npm run openspec:validate` (strict) passes
- [ ] 7.4 **Maintainer-side real-winget smoke (Windows):** apply 2 apps → gen 1; apply a 3rd →
      gen 2; `rollback --to 1 --confirm` → uninstalls the 3rd; `generations` shows the appended
      rollback-marked gen; `rollback` without `--confirm` refuses; already-absent ref is a no-op
