## Context

- `provision.HomeGenRef` (`internal/provision/provision.go`) records what the engine activated:
  `Flake` (always: the activated ref), `Config` (PR #89: the user's declared `home.nix` path when
  the flake was engine-generated from it), `Generation` (the resulting hm generation number).
- `recoverHomeManager` (`internal/commands/capture_realizer.go`) iterates `provision.List()`
  newest-first and currently prefers Config over Flake (PR #89). Settings adds a third tier.
- `manifest.HomeManagerSettings` (`internal/manifest/types.go`, PR #91) is the declarative catalog
  struct. It is a stdlib-only leaf — verified: `go list -deps ./internal/manifest/` shows zero
  endstate imports. Therefore `provision → manifest` is cycle-free and we can store
  `*manifest.HomeManagerSettings` directly (no serialized form needed).
- The existing guard test (`TestPackageStaysInstallOnly`) checks for `internal/restore` imports
  only; adding `internal/manifest` does not trigger it.

## Goals / Non-Goals

**Goals:**
- `capture` emits `homeManager.settings` for a settings-applied machine so it round-trips through
  the #91 apply settings stage.
- Zero regression on flake, config, package, and Windows paths.
- Hermetic tests; no live Nix invocations.

**Non-Goals:**
- No live-system settings recovery — not possible; only provisioning history.
- No new manifest schema — reuse `manifest.HomeManagerSettings`.
- No `--enable-restore` gate on capture — capture is read-only.
- No capture of settings applied outside Endstate (documented limitation).

## Decisions

- **Direction: record-on-apply, read-back-on-capture** — mirrors PR #89's Config fix exactly.
  `apply` records the declared settings into `HomeGenRef.Settings`; `capture` reads it back.
  Rejected live-system synthesis (not possible: hm generation has no source settings).
- **`provision → manifest` import is safe** — manifest is a stdlib-only leaf (confirmed above).
  Use `*manifest.HomeManagerSettings` directly; no serialized form.
- **Preference order: Settings > Config > Flake** — Settings is the most-specific input
  (user never wrote Nix); Config is next (user wrote a home.nix); Flake is the fallback
  (user wrote the full flake). The most-recent non-nil generation with any of these set wins.
- **Best-effort** — history error / absent Settings falls through the chain; never fails capture.

## Design

### 1. `provision.HomeGenRef` — add `Settings` field

```go
// Settings holds the user's declared homeManager.settings catalog when the
// activated config was compiled from it. Empty for config or flake inputs.
// Recorded alongside Flake (which carries the generated, machine-local ref)
// so capture can round-trip the portable settings declaration.
Settings *manifest.HomeManagerSettings `json:"settings,omitempty"`
```

`internal/provision/provision.go` gains an import of
`github.com/Artexis10/endstate/go-engine/internal/manifest`.

### 2. `apply_realizer.go` — record settings in the config stage

In the settings branch of `resolveHomeFlake` / `runApplyRealizer`, after building `homeRef`:

```go
homeRef = &provision.HomeGenRef{Flake: flake, Generation: hmGen}
if generated {
    homeRef.Config = mf.HomeManager.Config
}
// NEW: record declared settings so capture can round-trip them.
if mf.HomeManager.Settings != nil {
    homeRef.Settings = mf.HomeManager.Settings
}
```

(The `generated` flag is true for both Config and Settings branches; both set `homeRef.Config` for
the config path. For Settings, `mf.HomeManager.Config` is empty, so Settings is the right record.)

Actually the cleanest form: replace the existing `generated` block with explicit branch checks:

```go
homeRef = &provision.HomeGenRef{Flake: flake, Generation: hmGen}
switch {
case mf.HomeManager.Settings != nil:
    homeRef.Settings = mf.HomeManager.Settings
case mf.HomeManager.Config != "":
    homeRef.Config = mf.HomeManager.Config
}
```

### 3. `capture_realizer.go` — `recoverHomeManager` prefers Settings > Config > Flake

```go
func recoverHomeManager(flags CaptureFlags) *manifest.HomeManagerConfig {
    if gens, err := listGenerationsFn(); err == nil {
        for _, g := range gens {
            if g.HomeManager == nil {
                continue
            }
            if g.HomeManager.Settings != nil {
                return &manifest.HomeManagerConfig{Settings: g.HomeManager.Settings}
            }
            if g.HomeManager.Config != "" {
                return &manifest.HomeManagerConfig{Config: g.HomeManager.Config}
            }
            if g.HomeManager.Flake != "" {
                return &manifest.HomeManagerConfig{Flake: g.HomeManager.Flake}
            }
        }
    }
    // --update: preserve existing manifest's homeManager when history has none
    if flags.Update && flags.Manifest != "" {
        if mf, loadErr := loadManifest(flags.Manifest); loadErr == nil && mf.HomeManager != nil {
            return mf.HomeManager
        }
    }
    return nil
}
```

## Risks / Verification

- **Hermetic tests** — inject `listGenerationsFn` (existing seam): settings generation → emitted as
  settings; settings > config > flake precedence per most-recent non-nil generation; no settings in
  history → falls through to config/flake; history error → omitted + no failure.
- **Provision round-trip test** — `TestWriteTo_RecordsHomeManagerSettings`: write a generation with
  `HomeGenRef.Settings`, read it back, assert the struct matches.
- **Guard test** — `TestPackageStaysInstallOnly` checks for `internal/restore` only; adding
  `internal/manifest` does not trigger it. Run to confirm.
- **`GOOS=windows` build/vet** — Settings path is Nix-only; Windows path never reaches it.
- **Real-nix round-trip smoke** (Linux dev box): apply a manifest with `homeManager.settings`
  (`--enable-restore`) → provision generation records Settings → `capture` → manifest carries
  `homeManager.settings` → apply the captured manifest re-activates the same settings config.
