> TDD: write each test RED first, then implement to green. All CI tests are hermetic (no real `nix`). The single real-Nix step is the anchor harvest (a merge gate), not a CI gate.

## 0. Toolchain (this WSL box)

- [ ] 0.1 Install Go via Nix (`nix profile add nixpkgs#go`) so `cd go-engine && go test ./...` and `GOOS=windows go build ./...` run on Linux
- [ ] 0.2 Confirm `npm run openspec:validate` (strict) passes for this change before implementing

## 1. Error code (additive)

- [ ] 1.1 RED test: `envelope.ErrRealizerUnavailable == "REALIZER_UNAVAILABLE"` → add const beside `ErrWingetNotAvailable`

## 2. Realizer interface (beside Driver)

- [ ] 2.1 `internal/realizer/realizer.go` — `Realizer` interface (`Name`/`Current`/`Plan`/`Realize`) + `Installable`/`Element`/`Set`/`Diff`/`Result`/`Error` types; `Realize` takes `ToAdd` only
- [ ] 2.2 Compile-time assertion test: nix backend satisfies `realizer.Realizer`; assert nix does **not** implement `driver.Driver` (it is beside, not a driver)

## 3. Nix backend internals (hermetic, injected runner)

- [ ] 3.1 `internal/realizer/nix/profile.go` + RED tests: parse `nix profile list --json` v3 name-keyed object **and** legacy array → `Set`; compute `Diff` (desired − Current)
- [ ] 3.2 `internal/realizer/nix/internaljson.go` + RED tests: parse `@nix {...}` internal-json stderr lines → events; track started activities + `generationAdvanced`
- [ ] 3.3 **Harvest real anchors (merge gate):** capture Nix 3.21.0 stderr for eval / network / daemon / permission into `testdata/*.stderr`; **force** an equal-priority collision to capture (or document the structural-only path)
- [ ] 3.4 `internal/realizer/nix/classify.go` + RED contract test `classify_contract_test.go`: feed each `testdata/*.stderr` (+ exit, gen-advanced) → assert exact `(Code, Subcode)`. `classify` is the **single** source of the code incl. spawn → `REALIZER_UNAVAILABLE`. Structural-first; anchors only for daemon/permission/eval
- [ ] 3.5 `internal/realizer/nix/nix_other.go` (`//go:build !windows`) real exec of `nix profile add <toAdd...> --log-format internal-json` / `nix profile list --json`; `nix_windows.go` (`//go:build windows`) stub returning `REALIZER_UNAVAILABLE`
- [ ] 3.6 Ref resolution + pinning + RED tests: bare attr → `github:NixOS/nixpkgs/<pinned-rev>#attr`; explicit flakeref passthrough; pin constant chosen and documented
- [ ] 3.7 `Realize`/`Current`/`Plan` RED tests via injected runner: success advances gen; partial/commit failure leaves gen intact; `Current` round-trips the list fixture

## 4. Selection seam

- [ ] 4.1 `internal/commands/select.go` — add `selectRealizer(goos)` (linux/darwin → `nix.New()`; else `ErrNoRealizer`); `selectBackend` byte-unchanged
- [ ] 4.2 `internal/commands/verify.go` — add `newRealizerFn` injection seam beside `newDriverFn`
- [ ] 4.3 RED tests: `selectRealizer("linux"/"darwin")` non-nil; `selectRealizer("windows") == ErrNoRealizer`; `selectBackend("windows")` still winget

## 5. Apply / verify / plan fan-out

- [ ] 5.1 `internal/commands/apply_realizer.go` — `runApplyRealizer`: plan (`present`/`to_install`) → `installing` per `ToAdd` → one `Realize(ToAdd)` → fan out terminal events; engine-authored messages; raw text → `error.detail` only; systemic codes (`REALIZER_UNAVAILABLE`/`PERMISSION_DENIED`) return a top-level `*envelope.Error`
- [ ] 5.2 `internal/commands/apply.go` — ~3-line early fork: `if r, err := newRealizerFn(); err == nil { return runApplyRealizer(...) }` (winget loop below untouched)
- [ ] 5.3 `internal/commands/verify_realizer.go` + `plan_realizer.go` — fan-out using `Current()`/`Plan`; `plan_realizer.go` populates `planner.PlanResult` (Plan + Actions) from the `Diff`
- [ ] 5.4 RED tests (injected realizer fake): whole-set apply emits one `installing` + one terminal per ref and one summary; atomic failure → all `ToAdd` `failed`; `--dry-run` calls `Plan` not `Realize`; app with no Nix ref is `skipped`

## 6. Capabilities

- [ ] 6.1 `internal/commands/capabilities.go` — `driversFor` consults `selectRealizer` → `["nix"]` on linux/darwin
- [ ] 6.2 RED tests: `driversFor("linux") == ["nix"]`; `driversFor("windows") == ["winget"]`

## 7. Windows no-regression

- [ ] 7.1 `TestSelectBackend_WindowsUnchanged` (winget; not a realizer) + `TestDriversFor_WindowsWinget` + golden `TestPlatformInfo_WindowsByteIdentical`
- [ ] 7.2 `TestApply_WindowsTakesDriverPath` — mock driver (not a realizer) → existing per-item winget event sequence unchanged
- [ ] 7.3 `GOOS=windows go build ./...` green; existing `winget_test.go` untouched and green

## 8. Contract documentation (PROTECTED — needs explicit go-ahead)

- [ ] 8.1 `docs/contracts/cli-json-contract.md` §"Standard Error Codes" — add `REALIZER_UNAVAILABLE` row (additive)
- [ ] 8.2 `docs/contracts/cli-json-contract.md` capabilities note — one-line clarification that `platform.drivers` is `["nix"]` on Linux/macOS (additive)

## 9. Verification

- [ ] 9.1 `cd go-engine && go test ./...` green on Linux
- [ ] 9.2 `npm run openspec:validate` (strict) passes
- [ ] 9.3 Manual real-Nix smoke (not CI): `apply` a small manifest (e.g. `ripgrep`) on this WSL box; confirm install, `present` on re-run (idempotent), and `REALIZER_UNAVAILABLE`/`PERMISSION_DENIED` surfacing via the provocations
