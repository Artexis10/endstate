> TDD: write each test RED first, then implement to green. Hermetic + host-aware (inject
> `newRealizerFn`/`newDriverFn`; key fixtures by `runtime.GOOS`). Verify: `cd go-engine && go
> test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`. Real-nix prune smoke is
> runnable on this WSL box.

## 0. Gate

- [x] 0.1 `npm run openspec:validate` (strict) passes for this change
- [x] 0.2 **PAUSE for maintainer review of this spec** before any Go is written

## 1. `realizer.Pruner` + Nix `Remove`

- [x] 1.1 `internal/realizer/realizer.go`: add optional `Pruner` interface
      (`Remove(names []string) (Result, error)`)
- [x] 1.2 RED tests (`internal/realizer/nix/prune_test.go`, injected `Runner`): `Remove([a,b])`
      emits `profile remove --profile <p> a b`; advance → success; non-zero/spawn → classified
      `*realizer.Error` (REALIZER_UNAVAILABLE on spawn/daemon)
- [x] 1.3 `internal/realizer/nix/prune.go`: `Remove(names)` via `nix profile remove`; classify
      via the existing anchor path; compile-time assert `*nix.Backend` satisfies `realizer.Pruner`

## 2. Error code

- [x] 2.1 `internal/envelope/errors.go`: `ErrConvergenceUnsupported = "CONVERGENCE_UNSUPPORTED"`

## 3. apply `--prune` — realizer path

- [x] 3.1 `ApplyFlags.Prune bool`; thread through `RunApply` → `runApplyRealizer`
- [x] 3.2 Drift = `Current().Elements` whose leaf matches no desired ref leaf (invert
      `presentInSet`)
- [x] 3.3 `--dry-run` → include prune set in result, remove nothing; `--prune && !--confirm &&
      !--dry-run` → refuse (install results stand, nothing removed)
- [x] 3.4 `--prune && --confirm` → `Remove(drift)`; systemic → top-level envelope error; else
      `INSTALL_FAILED` (raw text in detail)
- [x] 3.5 If realizer is not a `Pruner` → `CONVERGENCE_UNSUPPORTED`

## 4. apply `--prune` — driver (winget) path refuses

- [x] 4.1 `internal/commands/apply.go`: `--prune` on the driver path → `CONVERGENCE_UNSUPPORTED`,
      change nothing

## 5. Generation records added + removed

- [x] 5.1 `apply_generation.go`: write a generation when `added>0 || removed>0`; populate
      `RemovedRefs` (reuse Phase-4 field); `Native`=final nix gen
- [x] 5.2 RED test: converged apply (install 1, prune 1) → one generation with both `addedRefs`
      and `removedRefs`; no-op convergence → no generation

## 6. Command tests (host-aware)

- [x] 6.1 `fakeRealizer` gains `Remove` (+ scripted result, captured args); a non-Pruner realizer
      stub for the unsupported case
- [x] 6.2 `apply --prune --confirm` removes drift; `--dry-run` previews, removes nothing;
      `--prune` without `--confirm` refuses, install unaffected
- [x] 6.3 Non-realizer (driver) `--prune` → `CONVERGENCE_UNSUPPORTED`
- [x] 6.4 Default `apply` (no `--prune`) removes nothing — `Remove` never called
- [x] 6.5 Guard: prune path does not import `internal/restore`

## 7. Dispatch + usage + contract (PROTECTED — maintainer-approved, additive)

- [x] 7.1 `cmd/endstate/main.go`: parse `--prune` → `ApplyFlags.Prune`; usage line
- [x] 7.2 `docs/contracts/cli-json-contract.md`: document `apply --prune` (prune actions in the
      result + `CONVERGENCE_UNSUPPORTED`)

## 8. Verification

- [x] 8.1 `cd go-engine && go test ./...` green on Linux
- [x] 8.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [x] 8.3 `npm run openspec:validate` (strict) passes
- [x] 8.4 Real-nix prune smoke: apply [jq,ripgrep] → gen; apply [jq] `--prune --confirm` →
      ripgrep removed, `nix profile list` shows only jq, generation records `removedRefs`;
      `--prune` without `--confirm` refuses; default apply removes nothing
