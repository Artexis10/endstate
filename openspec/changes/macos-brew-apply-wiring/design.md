## Context

On darwin the apply gate is `apply.go`: `if rz, _ := newRealizerFn(); rerr == nil { return
runApplyRealizer(...) }`. `selectRealizer("darwin") → nix.New()` always succeeds, so the realizer owns
config + all apps + the generation write + the event stream, and the winget-style driver loop below is
unreachable (`selectBackend("darwin") → ErrNoBackend`). The same `newRealizerFn`-first-return pattern
gates `verify.go`, `plan.go`, and `capture.go`. `manifest.App.Driver` already exists — no schema change.

So the two-lane split lives **at the realizer gate**, not at `selectBackend`. The brew driver is
**additive** to the realizer on darwin (both are live), unlike `selectBackend`/`selectRealizer` which
are mutually exclusive.

## Goals / Non-Goals

- **Goals:** install/capture/verify/plan `driver: "brew"` apps on darwin alongside the realizer in one
  run; keep a no-brew manifest byte-identical to today; keep one event stream with one summary per
  phase; record brew in its own provisioning generation; reject misrouted `cask:` refs at load.
- **Non-goals:** brew rollback, precise version pinning, the invisible bootstrap (all deferred).

## Decisions

### Third backend seam (additive)

- `select.go`: `ErrNoBrewDriver` + `selectBrewDriver(goos)` (`brew.New()` on darwin, else
  `ErrNoBrewDriver`). Additive — on darwin both `selectRealizer` and `selectBrewDriver` succeed.
- `verify.go`: a `newBrewDriverFn` package var (default `selectBrewDriver(runtime.GOOS)`), the test
  seam shared by apply/verify/plan/capture. It is resolved **only when the brew lane is non-empty**, so
  a no-brew manifest never touches it.

### Partition + manifest isolation

`apply_brew.go` `partitionBrewLane(apps) → (driver=="brew" lane, rest)`, order-preserving, case-
insensitive. At each gate, partition **after** `SynthesizeAppsFromModules` (so synthesized manual apps
land in the realizer lane), then hand the realizer a **shallow manifest copy** with `Apps = restApps`
(`rzMf := *mf; rzMf.Apps = restApps`). The realizer never sees a brew/`cask:` ref.

### Sequencing / isolation

Realizer lane FIRST (atomic generation, committed), brew lane SECOND (best-effort, per-package). A brew
install failure is a per-item `failed`, counted in the apply summary, and NEVER rolls back or aborts the
Nix generation. "brew absent" this increment = per-item visible skip (bootstrap deferred). A
`driver: "brew"` app on a non-darwin host is a visible `skipped`/`filtered` item (parity with the
realizer's manual-app handling), never silently dropped.

### One event stream + one summary per phase

The brew lane (`brewLane` in `apply_brew.go`) exposes per-phase methods (`planBrew`/`applyBrew`/
`verifyBrew`) that interleave brew per-item events INSIDE the realizer's already-open plan/apply/verify
phases and return counts that fold into the realizer's single `EmitSummary` per phase. No second set of
phase/summary events is ever emitted — the non-regression test compares the FULL stream.

### Provisioning

Brew writes a SEPARATE generation `backend: "brew"` after the Nix one (reusing
`writeProvisioningGeneration`, which no-ops when nothing was installed/removed — so a no-brew apply
records exactly ONE `nix` generation). Brew items are never merged into the Nix generation; the deferred
rollback gets clean per-backend data.

### Capture

`internal/driver/brew/capture.go` `EnumerateInstalled()` runs `brew leaves` (top-level formulae) +
`brew list --cask` (Casks) + best-effort `brew list --versions` (a missing version is `""`, never a
failure), in the driver package (hermetic via the existing `ExecCommand` fake). `runCaptureRealizer`
calls it on darwin and emits `driver: "brew"` apps (Casks as `cask:` refs), deduped by id against the
realizer-captured set. `capturedApp`/`cleanApp` gain a `driver` field preserved through capture, the
`--update` merge (existing apps re-read with their `Driver`), and sanitize — so a brew app round-trips.
Realizer-captured apps keep `Driver: ""`.

### Validation

`ValidateManifestApps` (runs in the loader on all OSes): a `cask:` darwin ref without `driver: "brew"`
→ `CASK_REF_REQUIRES_BREW_DRIVER`; `driver: "brew"` without a darwin ref →
`BREW_DRIVER_REQUIRES_DARWIN_REF`. The windows-brew-ref conflict check is deferred.

## Risks / Trade-offs

- **Event-stream contract:** the brew lane MUST interleave, never emit a 2nd summary per phase. Locked
  by `TestRunApply_NoBrewLane_ByteIdenticalToRealizerOnly` (full-stream comparison, timestamps
  normalized) and the brew-lane tests.
- **brew real-output anchors are ASSUMPTIONS** (`brew leaves`, `brew list --cask`, `brew list
  --versions` columns, install idempotency exit codes) validated ONLY by the macOS smoke; the hermetic
  tests lock the assumed shapes but do not prove real correctness.
- **Manifest aliasing:** the realizer gets a shallow copy with `restApps`; partition runs after
  synthesis so synthesized manual apps stay in the realizer lane.

## Migration

Additive. Existing manifests with no `driver: "brew"` apps are unaffected (non-regression test). The new
`driver` field on captured apps is `omitempty`, so existing captured manifests are unchanged.
