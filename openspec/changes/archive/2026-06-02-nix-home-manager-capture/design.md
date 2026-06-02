## Context

- `runCaptureRealizer` (`internal/commands/capture_realizer.go`) is the Nix capture path (#79): it
  reads the installed package set via `Current()` and writes a manifest of `capturedApp` entries via
  the local `captureManifestOutput{Version, Name, Captured, Apps}` struct in `capture.go`.
- `apply` (#81) activates `manifest.HomeManager.Flake` via `nix run <pin> -- switch --flake <ref>`
  and `apply_generation.go` records `provision.Generation.HomeManager = *HomeGenRef{Flake, Generation}`.
- `provision.List() ([]*Generation, error)` returns generations **newest-first**
  (`internal/provision/store.go`). `HomeGenRef.Flake` is the activated flakeref.
- Home-manager does **not** persist the originating flakeref in a live install (verified against the
  home-manager source: the generation records `hm-version` + a managed-file `manifest.json`, never the
  flake). So the engine's own provisioning history is the only automatable source.

## Goals / Non-Goals

**Goals:**
- `capture` emits the engine-provisioned home-manager flake so a captured manifest round-trips through
  the #81 apply config stage.
- Zero winget / package-capture / Windows regression; reuse existing types and the provisioning store.

**Non-Goals:**
- **No config *content* capture** — no raw-file snapshot, no `home.nix` synthesis (home-manager files
  are read-only store symlinks; can't re-apply through the flake stage; #81 declined this paradigm).
- **No live-system flake recovery** — not possible; the flake comes from provisioning history only.
- **No new manifest schema** — reuse `manifest.HomeManagerConfig`.
- **No `--enable-restore` gate** — capture is read-only.

## Decisions (maintainer, confirmed)

- **Direction A — engine-provisioned flake passthrough.** Recover the flake from provisioning history;
  emit `manifest.HomeManager`. Round-trips with apply; matches Endstate's "capture what it
  provisioned" model. Rejected raw-file snapshot (foreign paradigm, no round-trip) and hybrid (YAGNI).
- **Most-recent non-nil selection** — iterate `provision.List()` (newest-first), take the first
  generation with `HomeManager != nil && .Flake != ""`. A later package-only apply records
  `HomeManager=nil`, so this yields the currently-active config, not "latest overall".
- **Best-effort** — generation read errors / empty history omit the block; never fail capture.
- **Honest limitation (documented in spec):** only captures configs applied through Endstate; a
  pre-existing manual home-manager setup yields no flake until declared + applied once.

## Design

`captureManifestOutput` (capture.go) gains:

```go
HomeManager *manifest.HomeManagerConfig `json:"homeManager,omitempty"`
```

`runCaptureRealizer` (capture_realizer.go), after building `captured`/`outputApps` and before writing:

```go
var listGenerationsFn = provision.List // package var; tests override for hermeticity

func recoverHomeManager(flags CaptureFlags) *manifest.HomeManagerConfig {
    if gens, err := listGenerationsFn(); err == nil {
        for _, g := range gens { // newest-first
            if g.HomeManager != nil && g.HomeManager.Flake != "" {
                return &manifest.HomeManagerConfig{Flake: g.HomeManager.Flake}
            }
        }
    }
    // --update: preserve an existing manifest's homeManager when history has none
    if flags.Update && flags.Manifest != "" {
        if mf, err := loadManifest(flags.Manifest); err == nil && mf.HomeManager != nil {
            return mf.HomeManager
        }
    }
    return nil
}
```

`outManifest.HomeManager = recoverHomeManager(flags)` — omitempty drops it when nil. No event/contract
surface change; the package capture summary/result are unchanged.

## Risks / Verification

- **Hermetic tests** override `listGenerationsFn` (+ `withFakeRealizer`): flake present → emitted;
  most-recent-non-nil selection; none → omitted; `--update` preservation; read error → omitted + no
  failure; package capture unaffected.
- **Real-nix round-trip smoke** (Linux dev box, local home-manager pin): `apply` a tiny
  `homeConfigurations` flake (`--enable-restore`) → generation records the flake → `capture` → the
  manifest carries `homeManager.flake` → `apply` the captured manifest into fresh state re-activates.
  Proves the config loop closes. The live `switch` is the already-smoked #81 apply path; the new code
  (capture reading history) is fully covered hermetically.
- **No regression:** Nix-only path; Windows `newRealizerFn → ErrNoRealizer` never reaches it;
  `GOOS=windows` build/vet + the existing capture suite stay green.
