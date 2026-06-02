> TDD: write each test RED first, then implement to green. Hermetic + host-aware (inject a fake
> realizer via the shared `newRealizerFn` seam; key capture fixtures by `runtime.GOOS`;
> override BOTH `newRealizerFn` AND `newDriverFn` for command tests). Isolate state with
> `t.Setenv("ENDSTATE_ROOT", t.TempDir())`.
> Verify: `cd go-engine && go test ./...` (green) + `GOOS=windows go build ./... && go vet ./...`
> (clean) + `npm run openspec:validate` (strict).

## 1. Pure store-path version parser

- [ ] 1.1 RED tests (`internal/realizer/nix/versions_test.go`): realistic fixtures covering
      simple (`/nix/store/<hash>-ripgrep-14.1.0`), output suffix (`-14.1.0-bin`, `-14.1.0-man`),
      multi-segment version (`2.43.0.windows.1`), date version (`2025-04-01`), multi-path
      (prefers exact-name match), name mismatch (fallback), and unparseable (returns "")
- [ ] 1.2 `internal/realizer/nix/versions.go`: implement `StorePathVersion(name string, storePaths []string) string`

## 2. Capture: emit version into manifest App

- [ ] 2.1 Update `capturedApp` struct: add `Version string \`json:"version,omitempty"\``
- [ ] 2.2 Update `cleanApp` struct: add `Version string \`json:"version,omitempty"\``
- [ ] 2.3 `capture_realizer.go`: populate `Version` from `nix.StorePathVersion(el.Name, el.StorePaths)`
- [ ] 2.4 RED+green tests in `capture_realizer_test.go`: update `nixSet` helper to include
      realistic store paths; assert captured manifest `App.Version` matches parsed store-path version;
      assert empty-version element still produces a valid manifest entry (no error)

## 3. Apply: record version in Provisioning Generation

- [ ] 3.1 `apply_realizer.go`: for `present` entries, look up element in `cur` and set
      `action.Version = nix.StorePathVersion(...)` before the install phase
- [ ] 3.2 `apply_realizer.go`: for newly-`installed` entries, look up element in `res.After` and
      set `action.Version`
- [ ] 3.3 RED+green tests in `apply_realizer_test.go`: scripted `fakeRealizer` with elements
      carrying `StorePaths`; assert `ProvItem.Version` non-empty after apply; assert empty
      `StorePaths` → empty version (no error)

## 4. Verification

- [ ] 4.1 `cd go-engine && go test ./...` green
- [ ] 4.2 `GOOS=windows go build ./... && go vet ./...` clean
- [ ] 4.3 `npm run openspec:validate` (strict) passes
- [ ] 4.4 (Coordinator, real-nix) apply 2-3 packages → capture → assert `App.Version` non-empty
      and `ProvItem.Version` non-empty in the latest generation
