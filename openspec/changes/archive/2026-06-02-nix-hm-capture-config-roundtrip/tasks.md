> TDD: RED first, then implement. Hermetic + host-aware (inject `listGenerationsFn`; `GenerateHomeFlake`
> is pure-fs so the apply config path is hermetic). Verify: `cd go-engine && go test ./...` (Linux) +
> `GOOS=windows go build ./...` + `go vet`; real-nix config round-trip smoke (sandboxed `$HOME`, local
> hm pin via `git+file://`).

## 1. Record the declared input

- [x] 1.1 RED test (`apply_realizer_test.go`): a `homeManager.config` apply (`--enable-restore`,
      fakeRealizer HomeActivator, a real temp `home.nix`) records `gens[0].HomeManager.Config` =
      the declared config value (and `.Flake` = the generated ref); a `homeManager.flake` apply leaves
      `Config` empty (existing test still passes)
- [x] 1.2 `internal/provision/provision.go`: add `Config string` (`json:"config,omitempty"`) to
      `HomeGenRef`
- [x] 1.3 `internal/commands/apply_realizer.go`: when the config stage activated a `generated`
      (config-derived) flake, set `homeRef.Config = mf.HomeManager.Config`

## 2. Capture emits the declared input

- [x] 2.1 RED tests (`capture_realizer_test.go`, inject `listGenerationsFn`): a generation with
      `HomeManager.Config` set (and a generated `Flake`) → manifest carries `homeManager.config`
      (NOT the generated flake); a generation with only `Flake` → `homeManager.flake` (unchanged)
- [x] 2.2 `internal/commands/capture_realizer.go`: `recoverHomeManager` prefers a recorded `Config`
      over `Flake`

## 3. Verification

- [x] 3.1 `cd go-engine && go test ./...` green on Linux
- [x] 3.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [x] 3.3 `npm run openspec:validate` (strict) + `npx openspec validate nix-hm-capture-config-roundtrip --strict`
- [x] 3.4 Real-nix config round-trip smoke (sandboxed `$HOME`/`ENDSTATE_ROOT`, local hm pin via
      `git+file://`): `apply` a manifest with `homeManager.config` (a tiny `home.nix`) → `capture` →
      manifest carries `homeManager.config` (not a `state/` flake path) → `apply` the captured
      manifest re-activates
