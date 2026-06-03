> TDD: write each test RED first, then implement to green. Hermetic (inject `listGenerationsFn` +
> seeded `provision.Write`; no live Nix calls). Verify: `cd go-engine && go test ./...` (Linux) +
> `GOOS=windows go build ./...` + `go vet`; real-nix apply→capture→apply settings round-trip smoke.

## 1. Extend `provision.HomeGenRef` with `Settings`

- [ ] 1.1 RED test (`provision_test.go`): write a generation with `HomeGenRef.Settings` set → read
      back → `Settings` matches; confirm `TestPackageStaysInstallOnly` still passes with the new
      `manifest` import.
- [ ] 1.2 `internal/provision/provision.go`: add `Settings *manifest.HomeManagerSettings` to
      `HomeGenRef`; add import of `internal/manifest`.

## 2. Record `Settings` in `apply`

- [ ] 2.1 RED test (`apply_realizer_catalog_test.go`): after a settings apply, the provisioning
      generation's `HomeManager.Settings` equals the declared settings struct.
- [ ] 2.2 `internal/commands/apply_realizer.go`: in the config stage, after building `homeRef`,
      set `homeRef.Settings = mf.HomeManager.Settings` when the settings branch activated.

## 3. Emit `settings` in `capture`

- [ ] 3.1 RED tests (`capture_realizer_test.go`): a generation with Settings → manifest carries
      `homeManager.settings`; Settings > Config > Flake precedence; no Settings in history →
      falls through to Config/Flake; history error → omitted + still succeeds.
- [ ] 3.2 `internal/commands/capture_realizer.go`: extend `recoverHomeManager` to prefer Settings
      over Config over Flake.

## 4. Verification

- [ ] 4.1 `cd go-engine && go test ./...` green on Linux
- [ ] 4.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [ ] 4.3 `npm run openspec:validate` (strict) + `npx openspec validate nix-home-manager-catalog-capture --strict`
- [ ] 4.4 Real-nix round-trip smoke: apply a manifest with `homeManager.settings`
      (`--enable-restore`) → `capture` → manifest carries `homeManager.settings` → apply the
      captured manifest re-activates the settings config.
