## Context

`nix-home-manager-config` (#81/#87/#89) gave the engine the ability to apply and record a home-manager
configuration. The `verify` command checks packages and manifest `verify` entries but has no awareness of
home-manager config at all. A machine where the active home-manager generation diverges from the recorded one
silently reports all-green — a false positive. This change closes the verification gap.

## Decisions

- **Realizer path only.** The home-manager check is inside `runVerifyRealizer`. The winget/driver path
  (`RunVerify`'s else branch) is unchanged. The check fires only when `mf.HomeManager != nil`.
- **Active generation via a new optional seam.** The nix backend already has `homeGen()` (private). Rather
  than making it public or hardcoding it in the command layer, we introduce a new optional capability
  interface `realizer.HomeGenerationReader { ActiveHomeGeneration() int }`, discovered by type-assertion
  exactly like `Pruner`/`HomeActivator`/`HomeRollbacker`. The `*Backend` implements it. If the realizer
  does not implement `HomeGenerationReader`, the check is silently skipped (the backend cannot read hm
  state, so neither can we).
- **Recorded generation: newest non-nil HomeManager.** `provision.List()` is newest-first; skip entries
  with `HomeManager == nil` (package-only applies); the first match is the most-recently recorded config.
  This is the identical pattern used by `recoverHomeManager` in `capture_realizer.go`.
- **Three outcomes only.**
  - `pass` — active == recorded (or recorded == 0 and active == 0, meaning "declared but nothing ever
    applied" is... not actually checked here; see below).
  - `fail config_drift` — active > 0 AND active != recorded.
  - `fail missing` — active == 0 (home-manager was never applied or was torn down).
  - Edge: no recorded generation at all AND active == 0 → `fail missing` (declared in manifest but never
    applied). No recorded generation AND active > 0 → still `fail missing` (we treat "no history" as
    unverifiable presence without a reference → missing; this is the conservative side).
- **Reason constant.** Add `ReasonConfigDrift = "config_drift"` in `internal/driver/driver.go` alongside
  the existing `ReasonVersionDrift`. The `"missing"` reason already exists as `driver.ReasonMissing`.
- **VerifyItem shape.** `Type: "home-manager"`, `ID: "home-manager"` (stable, no ref). Active generation
  number in `Version` (as a decimal string), expected in `Expected`. This reuses the existing
  version-drift display path in consumers without schema changes.
- **`listGenerationsFn` seam.** `runVerifyRealizer` calls `listGenerationsFn()` (the same package-level
  var used by `capture_realizer.go`), which defaults to `provision.List` and is replaceable in tests.

## Design

### Realizer interface (`internal/realizer/realizer.go`)

```go
// HomeGenerationReader is an OPTIONAL realizer capability: reading the active
// home-manager generation number. Discovered by type-assertion on a Realizer,
// like Pruner / HomeActivator / HomeRollbacker. The Nix backend implements it;
// other backends (winget driver path) do not, and the hm verify check is skipped.
type HomeGenerationReader interface {
    ActiveHomeGeneration() int
}
```

### Nix backend (`internal/realizer/nix/home_manager.go`)

```go
var _ realizer.HomeGenerationReader = (*Backend)(nil)

func (b *Backend) ActiveHomeGeneration() int { return b.homeGen() }
```

### Verify path (`internal/commands/verify_plan_realizer.go`)

After the existing app loop and manifest verify entries, before the summary event:

```go
if mf.HomeManager != nil {
    if hgr, ok := r.(realizer.HomeGenerationReader); ok {
        item := checkHomeManagerGeneration(hgr, emitter)
        if item.Status == "pass" { pass++ } else { fail++ }
        results = append(results, item)
    }
}
```

`checkHomeManagerGeneration` reads the active generation, finds the recorded generation via
`listGenerationsFn()`, and builds the `VerifyItem`.

### Reason constant (`internal/driver/driver.go`)

```go
ReasonConfigDrift = "config_drift"
```

## Risks / Verification

- **No recorded history:** treated as `missing` (conservative — never silently pass without evidence).
- **Package-only applies between config applies:** `provision.List` newest-first; skip nil `HomeManager`
  entries; first non-nil is the config still in effect. Same as `recoverHomeManager`. Correct.
- **Windows CI cross-compile:** `ActiveHomeGeneration()` calls `homeGen()` which reads a symlink — pure
  stdlib, no platform guard needed. `GOOS=windows go build ./...` stays clean.
- **Hermetic tests:** `fakeRealizer` gains `activeHomeGen int` field + `ActiveHomeGeneration()` method;
  provision generations seeded via `provision.Write` under `t.Setenv("ENDSTATE_ROOT", t.TempDir())`.
  `listGenerationsFn` is the shared package var already used by the capture tests.
