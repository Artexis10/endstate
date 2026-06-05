# Design — best-effort brew rollback (two-lane, composed with native rollback)

## Context

`apply` on darwin already runs two lanes: the Nix realizer (default) commits an atomic generation, and a
best-effort brew lane installs `driver: "brew"` apps and records them in a SEPARATE `backend: "brew"`
provisioning generation (`apply_realizer.go`). `rollback`, by contrast, short-circuits: `RunRollback`
calls `newRealizerFn()` first and `runRealizerRollback` owns the whole operation — the brew driver
(`newBrewDriverFn`) is never consulted. So a darwin machine's brew apps are recorded but never rolled
back. This change makes `rollback` symmetric with `apply`: a brew uninstall lane composed into the
realizer rollback path.

The brew driver already implements `driver.Uninstaller` (`brew uninstall [--cask]`, never `--zap`,
already-absent → `StatusAbsent`). The winget best-effort rollback (`runDriverRollback`) already
implements the "uninstall the union of added refs of generations after the target" pattern. This change
reuses both — it adds wiring, not new driver or pattern code.

## Locked decisions

1. **The brew lane requires an explicit `--to N`.** Bare rollback (no `--to`) stays native-package-only
   and byte-identical to today. Rationale: the native "previous" anchor (`Rollback(-1)`) is
   nix-generation-relative and cannot be reconciled with interleaved `backend: "brew"` generations
   without an explicit engine-generation boundary. Composing brew into bare rollback would be ambiguous
   and host-state-dependent. This mirrors `--enable-restore` config rollback, which already requires
   `--to`. Bare-rollback brew support is a possible later increment.

2. **Composed into `runRealizerRollback`, not a separate dispatch.** Because the realizer path
   short-circuits `RunRollback`, the only way to reach brew is to compose the lane inside
   `runRealizerRollback` (a single merged `RollbackResult`). The brew set is computed from the resolved
   `targetGen` (engine generation number), the same boundary the native lane uses.

3. **Native first, then brew; brew never unwinds the native rollback.** The native package rollback (and
   opt-in config rollback) runs and is recorded first. The brew uninstall lane runs after. A brew
   per-package failure is partial (reported, run continues). A brew driver infrastructure error (e.g. no
   brew on a host that somehow recorded brew generations) is tolerated as failed refs rather than
   aborting — unlike `runDriverRollback`, which aborts, because here the native rollback has ALREADY
   committed and cannot be unwound. Best-effort posture, non-destructive.

4. **Brew-only target is valid.** A `--to N` whose generation has no native anchor (a brew generation) is
   accepted when `brewRemoveRefs` is non-empty; the native package rollback is skipped (no anchor) and
   only the brew lane runs. The "nothing to roll back" rejection (`!hasPackageTarget && homeRef == nil`)
   gains `&& len(brewRemoveRefs) == 0`.

5. **Reuse the existing `RollbackResult` best-effort fields.** Brew removals populate `RemovedRefs` /
   `FailedRefs` / `Partial` / `Warning` (today only the winget driver path uses them; the native path
   leaves them empty). This keeps the struct unchanged and the `omitempty` JSON clean when no brew lane
   ran — preserving the no-brew byte-identical guarantee. Brew uninstalls are recorded in a separate
   `backend: "brew"` rollback generation via the existing `appendRollbackGenerationRemoved`.

6. **The native rollback generation is only appended when the native lane did something.** Guarded to
   `hasPackageTarget || newHomeRef != nil`, so a brew-only rollback does not append an empty `backend:
   "nix"` generation (it appends only the `backend: "brew"` one). All pre-existing paths satisfy the
   guard, so their behavior is unchanged.

## Implementation sketch

- `brewRollbackRefs(targetGen int) []string` — union of `AddedRefs` of `Backend == "brew"` generations
  with `Number > targetGen`, de-duplicated, deterministic order (reuses `provision.List()`). Mirrors the
  `runDriverRollback` union, filtered to brew.
- `runBrewRollbackLane(refs []string) (removed, failed []string)` — resolves `newBrewDriverFn()`,
  type-asserts `driver.Uninstaller`, uninstalls each ref (per-package, failure-tolerant), and appends a
  `backend: "brew"` rollback generation. A missing/incapable brew driver reports all refs failed without
  aborting.
- `runRealizerRollback` changes: compute `brewRemoveRefs` (only when `flags.To != ""`); relax the
  nothing-to-rollback guard; include brew refs in the dry-run preview; after the native rollback + config
  + native generation append, run `runBrewRollbackLane` and fold its result into `RollbackResult`.

## Testing

Hermetic (the dev box is Linux; no real brew). Override BOTH seams — `newRealizerFn` (a capable
`fakeRealizer`) AND `newBrewDriverFn` (a `fakeBrewDriver` extended with `Uninstall`) — via the existing
`withRealizerAndBrew` helper; the seam gotcha is that overriding only one runs the wrong backend on a
given OS. Cases: brew refs uninstalled alongside native rollback; partial failure tolerated; dry-run
preview; confirm gate; brew-only target; **non-regression** — a no-brew history never resolves the brew
driver (`panicBrewDriverFn`) and the result is the native-only one; bare rollback leaves brew untouched.

## CI smoke wrinkle

The macOS runner has Homebrew preinstalled, so `scripts/smoke/brew-realbrew-smoke.sh` can apply a brew
formula, then `rollback --to N --confirm`, and assert the formula is uninstalled — the real uninstall
anchor. The script's `command -v brew` guard keeps the linux leg a no-op. The script path is already in
the `nix-integration.yml` path filter, so editing the script triggers the macOS smoke without touching
the workflow.
