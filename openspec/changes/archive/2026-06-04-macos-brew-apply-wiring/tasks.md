> TDD: each Go piece was written RED first, then implemented to green. All CI tests are hermetic (no
> real `brew`); brew's real-output anchors are confirmed ONLY by the macOS smoke (a CI leg, not a local
> gate on this Linux box).

## 1. Validation (cask routing)

- [x] 1.1 RED: a `cask:` darwin ref without `driver: "brew"` → `CASK_REF_REQUIRES_BREW_DRIVER`;
      `driver: "brew"` without a darwin ref → `BREW_DRIVER_REQUIRES_DARWIN_REF`; bare brew formula and
      plain nix apps validate cleanly; case-insensitive driver match.
- [x] 1.2 Implement `validateBrewApp` in `ValidateManifestApps` (host-independent, runs in the loader).

## 2. Third backend seam

- [x] 2.1 `select.go`: `ErrNoBrewDriver` + `selectBrewDriver(goos)` (additive — `brew.New()` on darwin).
- [x] 2.2 `verify.go`: `newBrewDriverFn` package var (default `selectBrewDriver(runtime.GOOS)`).
- [x] 2.3 Test seam: `withFakeRealizer`/`withMockDriver` default `newBrewDriverFn` to a fail-if-called
      fake so no unrelated test spawns real `brew`.

## 3. Partition helper

- [x] 3.1 RED: `partitionBrewLane` splits `driver=="brew"` (case-insensitive) from the rest, order-
      preserving.
- [x] 3.2 Implement `partitionBrewLane` in `apply_brew.go`.

## 4. Non-regression guard (written early)

- [x] 4.1 `TestRunApply_NoBrewLane_ByteIdenticalToRealizerOnly`: no-brew manifest → ApplyResult + full
      JSONL stream (timestamps normalized) match the realizer-only baseline.
- [x] 4.2 `TestRunApply_NoBrewLane_GateNeverResolvesBrewDriver`: the gate never calls `newBrewDriverFn`
      for a no-brew manifest and writes exactly ONE `nix` provisioning generation.

## 5. Brew apply lane

- [x] 5.1 RED: a `driver: "brew"` formula installs via the brew driver; a brew failure is a per-item
      `failed` that does not abort the committed Nix generation; a non-darwin host visibly skips.
- [x] 5.2 `apply_brew.go` `brewLane` with `planBrew`/`applyBrew`/`verifyBrew` interleaving into the
      realizer phases; `runApplyRealizer` gains `brewApps`/`brewDrv`; brew writes a separate
      `backend: "brew"` generation after the Nix one.

## 6. Verify / plan lanes

- [x] 6.1 RED: brew presence pass/fail in verify; brew install/none in plan; folded into the single
      summary.
- [x] 6.2 `runVerifyRealizer`/`runPlanRealizer` gain `brewApps`/`brewDrv`; `verifyBrewLane`/
      `planBrewLane` interleave + fold counts.

## 7. Capture lane

- [x] 7.1 RED (driver): `EnumerateInstalled` lists `brew leaves` + `brew list --cask` + best-effort
      versions; a missing version is `""`, not a failure; missing brew → `ErrBrewNotAvailable`.
- [x] 7.2 Implement `internal/driver/brew/capture.go`.
- [x] 7.3 RED (capture realizer): on darwin, capture emits `driver: "brew"` apps (Casks as `cask:`
      refs), deduped by id; non-darwin emits none.
- [x] 7.4 `capturedApp`/`cleanApp` gain a `driver` field preserved through capture, `--update` merge,
      and sanitize; `runCaptureRealizer` calls `EnumerateInstalled` on darwin.

## 8. macOS smoke + CI

- [x] 8.1 `scripts/smoke/brew-realbrew-smoke.sh`: `command -v brew` guard (linux leg no-op), build →
      manifest with a `driver: "brew"` formula `hello` + a tiny cask → apply (formula STRICT, cask
      best-effort) → capture (assert `driver:"brew"` + `hello` ref round-trip) → `brew uninstall` in the
      trap.
- [x] 8.2 `.github/workflows/nix-integration.yml`: extend the path filter + add a macOS-gated step
      (`if: runner.os == 'macOS'`, `continue-on-error` posture inherited).

## 9. OpenSpec + verification

- [x] 9.1 This change (`macos-brew-apply-wiring`) with distinct requirement names; `openspec validate
      --all --strict` green.
- [x] 9.2 `go test ./...`, `go vet ./...`, `GOOS=windows go build ./...` green.
- [ ] 9.3 macOS smoke confirmed green (CI-only; cannot run on the Linux dev box).
