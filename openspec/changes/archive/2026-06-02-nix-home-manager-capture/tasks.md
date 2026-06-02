> TDD: write each test RED first, then implement to green. Hermetic + host-aware (inject
> `listGenerationsFn` + a fake realizer via `withFakeRealizer`; key fixtures by `runtime.GOOS`).
> Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`; the
> real-nix apply→capture→apply config round-trip smoke runs on the Linux dev box (local hm pin).

## 1. Recover + emit the home-manager flake

- [x] 1.1 RED tests (`capture_realizer_test.go`, inject `listGenerationsFn` + `withFakeRealizer`):
      provisioning history whose newest config generation has a flake → manifest carries
      `homeManager.flake`; a newer generation with `HomeManager=nil` over an older one with a flake →
      the older flake is emitted (most-recent non-nil); no generation has a flake → no `homeManager`
      key; `listGenerationsFn` error → no block AND command still succeeds with packages captured
- [x] 1.2 `internal/commands/capture.go`: add `HomeManager *manifest.HomeManagerConfig`
      (`json:"homeManager,omitempty"`) to `captureManifestOutput`
- [x] 1.3 `internal/commands/capture_realizer.go`: add `var listGenerationsFn = provision.List`;
      `recoverHomeManager(flags)` selecting the most-recent non-nil `HomeManager.Flake`; set
      `outManifest.HomeManager`; best-effort (errors/empty omit the block)

## 2. --update preservation

- [x] 2.1 RED test: `--update` + `--manifest` with an existing `homeManager` block and no flake in
      history → the existing block is preserved in the output
- [x] 2.2 `recoverHomeManager`: when history has no flake, fall back to the existing manifest's
      `HomeManager` (via `loadManifest`)

## 3. Verification

- [x] 3.1 `cd go-engine && go test ./...` green on Linux
- [x] 3.2 `GOOS=windows go build ./...` + `go vet ./...` clean (winget + package capture untouched)
- [x] 3.3 `npm run openspec:validate` (strict) + `npx openspec validate nix-home-manager-capture --strict`
- [x] 3.4 Real-nix config round-trip smoke (isolated `ENDSTATE_ROOT`,
      `ENDSTATE_HOME_MANAGER_PIN=/home/hugoa/projects/home-manager`): `apply` a tiny
      `homeConfigurations` flake (`--enable-restore`) → `capture` → manifest carries
      `homeManager.flake` → `apply` the captured manifest into fresh state re-activates the config
