## Why

The Nix realizer now has package `apply`/`verify`, package `capture` (#79, the apply↔capture loop
closed for packages), and a home-manager **config apply** stage (#81: `nix run <pin> -- switch
--flake <ref>`, recorded in the Provisioning Generation). But `capture` is **package-only** — it does
not record the activated home-manager config. So the *config* half of the snapshot → manifest →
rebuild loop is open: you can apply a home-manager config but cannot snapshot one back into a
manifest.

This change closes it: `capture` recovers the home-manager flake the engine activated and emits it
into the captured manifest, so a captured manifest round-trips through #81's apply config stage.

**The flake is recovered from the engine, not the system.** Home-manager does not persist the
originating flakeref in a live install (the generation derivation records `hm-version` and a
`manifest.json` of managed files, but never the source flake — confirmed against the home-manager
source). However, Endstate's `apply` already records it: `apply_generation.go` writes
`provision.Generation.HomeManager = {Flake, Generation}`. Capture reads that provisioning history.

## What Changes

- **Recover the config flake in the realizer capture path.** `runCaptureRealizer` reads
  `provision.List()` (newest-first) and takes the flake from the **most-recent generation whose
  `HomeManager` is non-nil** (a later package-only apply records `HomeManager=nil`, so "latest
  overall" would wrongly drop it).
- **Emit it into the manifest.** The captured manifest gains
  `"homeManager": { "flake": "<ref>" }` when a flake is found — reusing the existing
  `manifest.HomeManagerConfig` type, so the output is a valid manifest the #81 apply path consumes
  unchanged. Absent ⇒ the block is omitted.
- **`--update` preservation.** When updating an existing manifest, prefer the flake from provisioning
  history; if history has none, preserve the existing manifest's `homeManager` block rather than
  dropping it.
- **Best-effort and non-destructive.** Reading generations is best-effort (mirrors run-history): a
  read error or empty history simply omits the block; package capture proceeds and the command never
  fails on this account. Capture has no `--enable-restore` gate (capture is read-only; the gate
  exists on apply because it mutates).

## Capabilities

### New Capabilities

- `nix-home-manager-capture`: `capture` records the engine-provisioned home-manager flake (from the
  Provisioning Generation history) and emits it into the manifest, so a captured manifest round-trips
  through the apply config stage. Home-manager config *content* capture remains out of scope.

### Modified Capabilities

- None. This extends `capture` within the existing contract (it emits a manifest field the apply
  config stage already defines). The winget capture path and package capture are unchanged.

## Impact

- `internal/commands/capture_realizer.go` — recover the flake from `provision.List()` and attach it
  to the output manifest; add a `listGenerationsFn` seam for hermetic tests (main change).
- `internal/commands/capture.go` — add an `omitempty` `HomeManager` field to `captureManifestOutput`
  (one field; winget path otherwise untouched).
- `internal/commands/capture_realizer_test.go` — hermetic, host-aware tests.
- **Zero winget / Windows / package-capture regression.** The realizer path is Nix-only; on Windows
  `newRealizerFn` returns `ErrNoRealizer` and capture never reaches this code. Proven by host-aware
  tests + `GOOS=windows` build/vet. Real-nix round-trip smoke (apply hm flake → capture → apply into
  fresh state) run on the Linux dev box against a local home-manager pin.
