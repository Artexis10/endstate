> TDD: write each test RED first, then implement to green. All CI tests are hermetic (no real
> `nix`/`winget`). Host-dependent tests are made host-aware (inject `newRealizerFn`; key
> fixtures by `runtime.GOOS`, per the Phase-1/2 `nixApp`/`foreignRefApp`/`withFakeRealizer`
> pattern). Verify on Linux: `cd go-engine && go test ./...`; plus `GOOS=windows go build ./...`
> + `go vet` clean.

## 0. Gate

- [ ] 0.1 `npm run openspec:validate` (strict) passes for this change before implementing
- [ ] 0.2 **PAUSE for maintainer review of this spec** (Gate A) before any Go is written

## 1. `provision.Generation` — rollback marker (additive)

- [ ] 1.1 Add optional `Rollback bool \`json:"rollback,omitempty"\`` to `Generation`
- [ ] 1.2 RED test: round-trips; absent when false (omitempty); `SchemaVersion` stays `"1.0"`

## 2. Engine error codes (additive)

- [ ] 2.1 `internal/envelope/errors.go`: `ErrRollbackUnsupported = "ROLLBACK_UNSUPPORTED"`,
      `ErrGenerationNotFound = "GENERATION_NOT_FOUND"`, `ErrRollbackFailed = "ROLLBACK_FAILED"`

## 3. `provision.Rollbacker` on the Nix realizer

- [ ] 3.1 RED tests (`internal/realizer/nix/rollback_test.go`, injected `Runner`/`fakeRun`):
      bare rollback emits `profile rollback --profile <p>` (no `--to`); `Rollback(4)` emits
      `--to 4`; non-zero exit → classified `*realizer.Error`; spawn error → REALIZER_UNAVAILABLE
- [ ] 3.2 `internal/realizer/nix/rollback.go`: `func (b *Backend) Rollback(to int) error` via
      `nix profile rollback [--to N]`; classify failures with `classify(...)`; raw text → detail
- [ ] 3.3 Compile-time assertion that `*nix.Backend` satisfies `provision.Rollbacker`

## 4. `rollback` command

- [ ] 4.1 `internal/commands/rollback.go`: `RollbackFlags{To string; Confirm, DryRun bool; Events string}`,
      `RollbackResult`, `RunRollback`
- [ ] 4.2 Acquire realizer via `newRealizerFn()`; `err != nil` → `ROLLBACK_UNSUPPORTED`
- [ ] 4.3 Gate on `provision.Rollbacker` + `CapabilityReporter.Capabilities().NativeRollback`;
      else `ROLLBACK_UNSUPPORTED`
- [ ] 4.4 Target resolution: `--to N` → `provision.List()` find `Number==N`; missing or no
      `Native` → `GENERATION_NOT_FOUND`; `native = atoi(Native)`. No `--to` → `native = -1`
- [ ] 4.5 `--dry-run` → preview result, no `Rollback` call, no generation written. No `--confirm`
      (and not dry-run) → refuse with remediation, no mutation
- [ ] 4.6 Execute `rb.Rollback(native)`; systemic (`isSystemic`) → top-level envelope error;
      else `ROLLBACK_FAILED`; raw text only in `error.detail` (the moat)
- [ ] 4.7 On success: `r.Current()` → append a rollback-marked generation (Items=present set,
      `AddedRefs=[]`, `Native`=active version, `RunID="rollback-<ts>"`, `Rollback=true`); return
      `RollbackResult`

## 5. Command tests (`internal/commands/rollback_test.go`, host-aware)

- [ ] 5.1 Extend `fakeRealizer` with `Rollback(to int) error` (+ `rollbackErr`, `lastRollbackArg`)
      and a configurable `Capabilities()` (`CapabilityReporter`)
- [ ] 5.2 No realizer (inject `ErrNoRealizer`) → `ROLLBACK_UNSUPPORTED`
- [ ] 5.3 Realizer with `NativeRollback:false` → `ROLLBACK_UNSUPPORTED`
- [ ] 5.4 `--to N` present (seed a generation file with `Native`) → `Rollback(native)` called;
      new generation appended with `Rollback:true`, `AddedRefs` empty
- [ ] 5.5 `--to N` missing / no native anchor → `GENERATION_NOT_FOUND`; no mutation
- [ ] 5.6 Bare rollback → `Rollback(-1)`
- [ ] 5.7 No `--confirm`, not dry-run → refuses, `Rollback` not called, no generation written
- [ ] 5.8 `--dry-run` → `Rollback` not called, no generation written
- [ ] 5.9 Systemic error → envelope error, raw text only in `Detail` (not `Message`)
- [ ] 5.10 Guard test: the rollback command path does not import `internal/restore`

## 6. Dispatch + usage (PROTECTED — maintainer-approved, additive)

- [ ] 6.1 `cmd/endstate/main.go`: `case "rollback"` in `dispatch()` mapping
      `--to`/`--confirm`/`--dry-run`/`--events`; add `rollback` to `usageText` +
      `commandUsage("rollback")`

## 7. Contract documentation (PROTECTED — maintainer-approved, additive)

- [ ] 7.1 `docs/contracts/cli-json-contract.md`: `## Command: rollback` (flags + `RollbackResult`
      shape, "additive in schema 1.x"); add the three error codes to the Standard Error Codes
      table; note the optional `rollback` field on generation records
- [ ] 7.2 `docs/ai/AI_CONTRACT.md` (PROTECTED, maintainer-approved): reword Non-Goal #4 to note a
      manual, opt-in `rollback` exists while preserving the no-*automatic*-rollback principle
- [ ] 7.3 `specs/separation-of-concerns/spec.md` (this change): ADDED requirement "Rollback
      operates on packages only" (distinct requirement — no MODIFY conflict)

## 8. Windows no-regression

- [ ] 8.1 Test: on a no-realizer host fixture, `rollback` returns `ROLLBACK_UNSUPPORTED` and
      writes/changes nothing
- [ ] 8.2 `GOOS=windows go build ./...` + `go vet ./...` clean; existing tests green

## 9. Verification

- [ ] 9.1 `cd go-engine && go test ./...` green on Linux
- [ ] 9.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [ ] 9.3 `npm run openspec:validate` (strict) passes
- [ ] 9.4 Real-nix smoke (not CI): `apply` 2-pkg manifest → gen N; add a 3rd → gen N+1;
      `rollback --to <N> --confirm` → back to the 2-pkg set; `generations` shows the appended
      rollback-marked generation as newest; `rollback` without `--confirm` refuses; daemon-down →
      `REALIZER_UNAVAILABLE`
